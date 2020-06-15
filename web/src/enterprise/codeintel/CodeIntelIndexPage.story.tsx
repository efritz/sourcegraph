import { storiesOf } from '@storybook/react'
import React from 'react'
import * as H from 'history'
import { CodeIntelIndexPage, Index } from './CodeIntelIndexPage'
import webStyles from '../../SourcegraphWebApp.scss'
import { of } from 'rxjs'
import * as GQL from '../../../../shared/src/graphql/schema'

window.context = {}

const { add } = storiesOf('web/CodeIntelIndex', module).addDecorator(story => (
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

const index: Pick<Index, 'id' | 'projectRoot' | 'inputCommit'> = {
    id: '1234',
    projectRoot: {
        path: 'web/',
        commit: {
            url: '',
            oid: '9ea5e9f0e0344f8197622df6b36faf48ccd02570',
            abbreviatedOID: '9ea5e9f',
            repository: {
                url: '',
                name: 'github.com/sourcegraph/sourcegraph',
            },
        },
    },
    inputCommit: '9ea5e9f0e0344f8197622df6b36faf48ccd02570',
}

add('Completed', () => (
    <CodeIntelIndexPage
        {...commonProps}
        fetchLsifIndex={() =>
            of({
                ...index,
                state: GQL.LSIFIndexState.COMPLETED,
                queuedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: '2020-06-15T12:30:30+00:00',
                failure: null,
                placeInQueue: null,
            })
        }
    />
))

add('Errored', () => (
    <CodeIntelIndexPage
        {...commonProps}
        fetchLsifIndex={() =>
            of({
                ...index,
                state: GQL.LSIFIndexState.ERRORED,
                queuedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: '2020-06-15T12:30:30+00:00',
                failure: 'Whoops! The server encountered a booo-boo handling this input.',
                placeInQueue: null,
            })
        }
    />
))

add('Processing', () => (
    <CodeIntelIndexPage
        {...commonProps}
        fetchLsifIndex={() =>
            of({
                ...index,
                state: GQL.LSIFIndexState.PROCESSING,
                queuedAt: '2020-06-15T12:20:30+00:00',
                startedAt: '2020-06-15T12:25:30+00:00',
                finishedAt: null,
                failure: null,
                placeInQueue: null,
            })
        }
    />
))

add('Queued', () => (
    <CodeIntelIndexPage
        {...commonProps}
        fetchLsifIndex={() =>
            of({
                ...index,
                state: GQL.LSIFIndexState.QUEUED,
                queuedAt: '2020-06-15T12:20:30+00:00',
                startedAt: null,
                finishedAt: null,
                placeInQueue: 3,
                failure: null,
            })
        }
    />
))
