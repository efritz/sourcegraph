package client

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/sourcegraph/sourcegraph/internal/endpoint"
	"github.com/sourcegraph/sourcegraph/internal/env"
)

var (
	preciseCodeIntelAPIServerURL = env.Get("PCI_API_SERVER_URL", "k8s+http://precise-code-intel:3186", "precise-code-intel-api-server URL (or space separated list of precise-code-intel-api-server URLs)")

	preciseCodeIntelAPIServerURLsOnce sync.Once
	preciseCodeIntelAPIServerURLs     *endpoint.Map

	DefaultClient = &Client{
		endpoint: LSIFURLs(),
		HTTPClient: &http.Client{
			// nethttp.Transport will propagate opentracing spans
			Transport: &nethttp.Transport{},
		},
	}
)

type Client struct {
	endpoint   *endpoint.Map
	HTTPClient *http.Client
}

func LSIFURLs() *endpoint.Map {
	preciseCodeIntelAPIServerURLsOnce.Do(func() {
		if len(strings.Fields(preciseCodeIntelAPIServerURL)) == 0 {
			preciseCodeIntelAPIServerURLs = endpoint.Empty(errors.New("an precise-code-intel-api-server has not been configured"))
		} else {
			preciseCodeIntelAPIServerURLs = endpoint.New(preciseCodeIntelAPIServerURL)
		}
	})
	return preciseCodeIntelAPIServerURLs
}
