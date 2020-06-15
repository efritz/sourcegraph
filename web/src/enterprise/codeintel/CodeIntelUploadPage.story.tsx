import { CodeIntelUploadPage } from './CodeIntelUploadPage'
import { of } from 'rxjs'
import { storiesOf } from '@storybook/react'
import { SuiteFunction } from 'mocha'
import { Upload } from './backend'
import * as GQL from '../../../../shared/src/graphql/schema'
import * as H from 'history'
import React from 'react'
import webStyles from '../../SourcegraphWebApp.scss'

window.context = {} as SourcegraphContext & SuiteFunction

const { add } = storiesOf('web/CodeIntelUpload', module).addDecorator(story => (
    <>
        <style>{webStyles}</style>
        <div className="theme-light container">{story()}</div>
    </>
))

const history = H.createMemoryHistory()

const commonProps = {
    history,
    location: history.location,
    match: {
        params: { id: '' },
        isExact: true,
        path: '',
        url: '',
    },
}

const upload: Pick<Upload, 'id' | 'projectRoot' | 'inputCommit' | 'inputRoot' | 'inputIndexer' | 'isLatestForRepo'> = {
    id: '1234',
    projectRoot: {
        url: '',
        path: 'web/',
        repository: {
            url: '',
            name: 'github.com/sourcegraph/sourcegraph',
        },
        commit: {
            url: '',
            oid: '9ea5e9f0e0344f8197622df6b36faf48ccd02570',
            abbreviatedOID: '9ea5e9f',
        },
    },
    inputCommit: '9ea5e9f0e0344f8197622df6b36faf48ccd02570',
    inputRoot: 'web/',
    inputIndexer: 'lsif-tsc',
    isLatestForRepo: false,
}

add('Completed', () => (
    <CodeIntelUploadPage
        {...commonProps}
        fetchLsifUpload={() =>
            of({
                ...upload,
                state: GQL.LSIFUploadState.COMPLETED,
                uploadedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: '2020-06-15T12:30:30+00:00',
                failure: null,
                placeInQueue: null,
            })
        }
    />
))

add('Errored', () => (
    <CodeIntelUploadPage
        {...commonProps}
        fetchLsifUpload={() =>
            of({
                ...upload,
                state: GQL.LSIFUploadState.ERRORED,
                uploadedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: '2020-06-15T12:30:30+00:00',
                failure: 'Whoops! The server encountered a booo-boo handling this input.',
                placeInQueue: null,
            })
        }
    />
))

add('Processing', () => (
    <CodeIntelUploadPage
        {...commonProps}
        fetchLsifUpload={() =>
            of({
                ...upload,
                state: GQL.LSIFUploadState.PROCESSING,
                uploadedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: null,
                failure: null,
                placeInQueue: null,
            })
        }
    />
))

add('Queued', () => (
    <CodeIntelUploadPage
        {...commonProps}
        fetchLsifUpload={() =>
            of({
                ...upload,
                state: GQL.LSIFUploadState.QUEUED,
                uploadedAt: '2020-06-15T12:20:30+00:00',
                startedAt: null,
                finishedAt: null,
                placeInQueue: 3,
                failure: null,
            })
        }
    />
))

add('Uploading', () => (
    <CodeIntelUploadPage
        {...commonProps}
        fetchLsifUpload={() =>
            of({
                ...upload,
                state: GQL.LSIFUploadState.UPLOADING,
                uploadedAt: '2020-06-15T12:20:30+00:00',
                startedAt: null,
                finishedAt: null,
                failure: null,
                placeInQueue: null,
            })
        }
    />
))
