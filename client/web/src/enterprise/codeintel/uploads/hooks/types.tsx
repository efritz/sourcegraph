import { gql } from '@sourcegraph/shared/src/graphql/graphql'

export const lsifUploadFieldsFragment = gql`
    fragment LsifUploadFields on LSIFUpload {
        __typename
        id
        inputCommit
        inputRoot
        inputIndexer
        projectRoot {
            url
            path
            repository {
                url
                name
            }
            commit {
                url
                oid
                abbreviatedOID
            }
        }
        state
        failure
        isLatestForRepo
        uploadedAt
        startedAt
        finishedAt
        placeInQueue
        associatedIndex {
            id
            state
            queuedAt
            startedAt
            finishedAt
            placeInQueue
        }
    }
`
