package uploadstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

type s3Store struct {
	bucket       string
	ttl          time.Duration
	manageBucket bool
	client       s3API
	uploader     s3Uploader
}

var _ Store = &s3Store{}

// newS3FromConfig creates a new store backed by AWS Simple Storage Service.
func newS3FromConfig(ctx context.Context, config *Config) (Store, error) {
	return newS3(config.S3.Bucket, config.S3.TTL, config.ManageBucket)
}

// newS3 creates a new store backed by AWS Simple Storage Service.
func newS3(bucket string, ttl time.Duration, manageBucket bool) (Store, error) {
	sess, err := session.NewSessionWithOptions(awsSessionOptions())
	if err != nil {
		return nil, err
	}

	s3Client := s3.New(sess)
	api := &s3APIShim{s3Client}
	uploader := &s3UploaderShim{s3manager.NewUploaderWithClient(s3Client)}
	store := newS3WithClients(api, uploader, bucket, ttl, manageBucket)
	return store, nil
}

func newS3WithClients(client s3API, uploader s3Uploader, bucket string, ttl time.Duration, manageBucket bool) *s3Store {
	return &s3Store{
		bucket:       bucket,
		ttl:          ttl,
		manageBucket: manageBucket,
		client:       client,
		uploader:     uploader,
	}
}

func (s *s3Store) Init(ctx context.Context) error {
	if !s.manageBucket {
		return nil
	}

	if err := s.create(ctx); err != nil {
		return errors.Wrap(err, "failed to create bucket")
	}

	if err := s.update(ctx); err != nil {
		return errors.Wrap(err, "failed to update bucket attributes")
	}

	return nil
}

func (s *s3Store) Get(ctx context.Context, key string, skipBytes int64) (io.ReadCloser, error) {
	var bytesRange *string
	if skipBytes > 0 {
		bytesRange = aws.String(fmt.Sprintf("bytes=%d-", skipBytes))
	}

	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Range:  bytesRange,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get object")
	}

	return resp.Body, nil
}

func (s *s3Store) Upload(ctx context.Context, key string, r io.Reader) error {
	err := s.uploader.Upload(ctx, &s3manager.UploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	})

	return errors.Wrap(err, "failed to upload object")
}

func (s *s3Store) Compose(ctx context.Context, destination string, sources ...string) (err error) {
	multipartUpload, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(destination),
	})
	if err != nil {
		return errors.Wrap(err, "failed to create multipart upload")
	}

	defer func() {
		if err == nil {
			// Delete sources on success
			err = s.deleteSources(ctx, *multipartUpload.Bucket, sources)
			return
		}

		// On failure, try to clean up copied then orphaned parts
		_, abortErr := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   multipartUpload.Bucket,
			Key:      multipartUpload.Key,
			UploadId: multipartUpload.UploadId,
		})
		if abortErr != nil {
			err = multierror.Append(err, errors.Wrap(abortErr, "failed to abort multipart upload"))
		}
	}()

	var m sync.Mutex
	etags := map[int]*string{}

	if err := invokeParallel(sources, func(index int, source string) error {
		partNumber := index + 1

		copyResult, err := s.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
			Bucket:     multipartUpload.Bucket,
			Key:        multipartUpload.Key,
			UploadId:   multipartUpload.UploadId,
			PartNumber: aws.Int64(int64(partNumber)),
			CopySource: aws.String(fmt.Sprintf("%s/%s", s.bucket, source)),
		})
		if err != nil {
			return errors.Wrap(err, "failed to upload part")
		}

		m.Lock()
		etags[partNumber] = copyResult.CopyPartResult.ETag
		m.Unlock()

		return nil
	}); err != nil {
		return err
	}

	var parts []*s3.CompletedPart
	for i := 0; i < len(sources); i++ {
		partNumber := i + 1

		parts = append(parts, &s3.CompletedPart{
			ETag:       etags[partNumber],
			PartNumber: aws.Int64(int64(partNumber)),
		})
	}

	if _, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          multipartUpload.Bucket,
		Key:             multipartUpload.Key,
		UploadId:        multipartUpload.UploadId,
		MultipartUpload: &s3.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		return errors.Wrap(err, "failed to complete multipart upload")
	}

	return nil
}

func (s *s3Store) deleteSources(ctx context.Context, bucket string, sources []string) error {
	return invokeParallel(sources, func(index int, source string) error {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(source),
		}); err != nil {
			return multierror.Append(err, errors.Wrap(err, "failed to delete source object"))
		}

		return nil
	})
}

func (s *s3Store) create(ctx context.Context) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})

	codes := []string{
		s3.ErrCodeBucketAlreadyExists,
		s3.ErrCodeBucketAlreadyOwnedByYou,
	}

	for _, code := range codes {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == code {
			return nil
		}
	}

	return err
}

func (s *s3Store) update(ctx context.Context) error {
	configureRequest := &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(s.bucket),
		LifecycleConfiguration: s.lifecycle(),
	}

	_, err := s.client.PutBucketLifecycleConfiguration(ctx, configureRequest)
	return err
}

func (s *s3Store) lifecycle() *s3.BucketLifecycleConfiguration {
	days := aws.Int64(int64(s.ttl / (time.Hour * 24)))

	return &s3.BucketLifecycleConfiguration{
		Rules: []*s3.LifecycleRule{
			{
				ID:         aws.String("Expiration Rule"),
				Status:     aws.String("Enabled"),
				Filter:     &s3.LifecycleRuleFilter{Prefix: aws.String("")},
				Expiration: &s3.LifecycleExpiration{Days: days},
			},
			{
				ID:                             aws.String("Abort Incomplete Multipart Upload Rule"),
				Status:                         aws.String("Enabled"),
				Filter:                         &s3.LifecycleRuleFilter{Prefix: aws.String("")},
				AbortIncompleteMultipartUpload: &s3.AbortIncompleteMultipartUpload{DaysAfterInitiation: days},
			},
		},
	}
}

// awsSessionOptions returns the session used to configure the AWS SDK client.
//
// Authentication of the client will first prefer environment variables, then will
// fall back to a credentials file on disk. The following envvars specify an access
// and secret key, respectively.
//
// - AWS_ACCESS_KEY_ID or AWS_ACCESS_KEY
// - AWS_SECRET_ACCESS_KEY or AWS_SECRET_KEY
//
// If these variables are unset, then the client will read the credentails file at
// the path specified by AWS_SHARED_CREDENTIALS_FILE, or ~/.aws/credentials if not
// specified. The envvar AWS_PROFILE can be used to specify a non-default profile
// within the credentails file.
//
// To specify a non-default region or endpoint, supply the envvars AWS_REGION and
// AWS_ENDPOINT, respectively.
func awsSessionOptions() session.Options {
	return session.Options{
		Config: aws.Config{
			Credentials: credentials.NewCredentials(&credentials.ChainProvider{
				Providers: []credentials.Provider{
					&credentials.EnvProvider{},
					&credentials.SharedCredentialsProvider{},
				},
				VerboseErrors: true,
			}),
			Endpoint:         awsEnv("AWS_ENDPOINT"),
			Region:           awsEnv("AWS_REGION"),
			S3ForcePathStyle: aws.Bool(os.Getenv("AWS_S3_FORCE_PATH_STYLE") != ""),
		},
	}
}

func awsEnv(name string) *string {
	if value := os.Getenv(name); value != "" {
		return aws.String(value)
	}

	return nil
}
