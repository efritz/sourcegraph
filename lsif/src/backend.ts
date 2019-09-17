import * as fs from 'mz/fs'
import * as path from 'path'
import uuid from 'uuid'
import { ConnectionCache, DocumentCache, ResultChunkCache } from './cache'
import { Database } from './database'
import { DefinitionModel, DocumentModel, MetaModel, ReferenceModel, ResultChunkModel } from './models.database'
import { hasErrorCode } from './util'
import { importLsif } from './importer'
import { Readable } from 'stream'
import { XrepoDatabase } from './xrepo'

export const ERRNOLSIFDATA = 'NoLSIFDataError'

/**
 * An error thrown when no LSIF database can be found on disk.
 */
export class NoLSIFDataError extends Error {
    public readonly name = ERRNOLSIFDATA

    constructor(repository: string, commit: string) {
        super(`No LSIF data available for ${repository}@${commit}.`)
    }
}

/**
 * Backend for LSIF dumps stored in SQLite.
 */
export class Backend {
    constructor(
        private storageRoot: string,
        private xrepoDatabase: XrepoDatabase,
        private connectionCache: ConnectionCache,
        private documentCache: DocumentCache,
        private resultChunkCache: ResultChunkCache
    ) {}

    /**
     * Read the content of the temporary file containing a JSON-encoded LSIF
     * dump. Insert these contents into some storage with an encoding that
     * can be subsequently read by the `createRunner` method.
     */
    public async insertDump(input: Readable, repository: string, commit: string): Promise<void> {
        const outFile = path.join(this.storageRoot, 'tmp', uuid.v4())

        try {
            const { packages, references } = await this.connectionCache.withTransactionalEntityManager(
                outFile,
                [DefinitionModel, DocumentModel, MetaModel, ReferenceModel, ResultChunkModel],
                entityManager => importLsif(entityManager, input),
                async connection => {
                    await connection.query('PRAGMA synchronous = OFF')
                    await connection.query('PRAGMA journal_mode = OFF')
                }
            )

            // Update cross-repository database
            await this.xrepoDatabase.addPackagesAndReferences(repository, commit, packages, references)

            // Move the temp file where it can be found by the server
            await fs.rename(outFile, makeFilename(this.storageRoot, repository, commit))
        } catch (e) {
            await fs.unlink(outFile)
            throw e
        }
    }

    /**
     * Create a database relevant to the given repository and commit hash. This
     * assumes that data for this subset of data has already been inserted via
     * `insertDump` (otherwise this method is expected to throw).
     */
    public async createDatabase(repository: string, commit: string): Promise<Database> {
        const file = makeFilename(this.storageRoot, repository, commit)

        try {
            await fs.stat(file)
        } catch (e) {
            if (hasErrorCode(e, 'ENOENT')) {
                throw new NoLSIFDataError(repository, commit)
            }

            throw e
        }

        return new Database(
            this.storageRoot,
            this.xrepoDatabase,
            this.connectionCache,
            this.documentCache,
            this.resultChunkCache,
            repository,
            commit,
            file
        )
    }
}

/**
 * Create the path of the SQLite database file for the given repository and commit.
 *
 * @param storageRoot The path where SQLite databases are stored.
 * @param repository The repository name.
 * @param commit The repository commit.
 */
export function makeFilename(storageRoot: string, repository: string, commit: string): string {
    return path.join(storageRoot, `${encodeURIComponent(repository)}.lsif.db`)
}

export async function createBackend(
    storageRoot: string,
    connectionCache: ConnectionCache,
    documentCache: DocumentCache,
    resultChunkCache: ResultChunkCache
): Promise<Backend> {
    try {
        await fs.mkdir(storageRoot)
    } catch (e) {
        if (!hasErrorCode(e, 'EEXIST')) {
            throw e
        }
    }

    return new Backend(
        storageRoot,
        new XrepoDatabase(connectionCache, path.join(storageRoot, 'xrepo.db')),
        connectionCache,
        documentCache,
        resultChunkCache
    )
}
