import express from 'express'
import { Backend } from '../backend/backend'
import { Logger } from 'winston'
import { pipeline as _pipeline } from 'stream'
import { Tracer } from 'opentracing'
import { wrap } from 'async-middleware'
import { limitOffset } from '../pagination/limit-offset'
import { nextLink } from '../pagination/link'
import { DEFAULT_DUMP_PAGE_SIZE } from '../settings'

/**
 * Create a router containing the LSIF dump endpoints.
 *
 * @param backend The backend instance.
 * @param logger The logger instance.
 * @param tracer The tracer instance.
 */
export function createDumpRouter(backend: Backend, logger: Logger, tracer: Tracer | undefined): express.Router {
    const router = express.Router()

    router.get(
        '/dumps/:repository',
        wrap(
            async (req: express.Request, res: express.Response): Promise<void> => {
                const { repository } = req.params
                const { query, visibleAtTip: visibleAtTipRaw } = req.query
                const { limit, offset } = limitOffset(req, DEFAULT_DUMP_PAGE_SIZE)
                const visibleAtTip = visibleAtTipRaw === 'true'
                const { dumps, totalCount } = await backend.dumps(
                    decodeURIComponent(repository),
                    query,
                    visibleAtTip,
                    limit,
                    offset
                )

                if (offset + dumps.length < totalCount) {
                    res.set('Link', nextLink(req, { limit, offset: offset + dumps.length }))
                }

                res.json({ dumps, totalCount })
            }
        )
    )

    router.get(
        '/dumps/:repository/:id',
        wrap(
            async (req: express.Request, res: express.Response): Promise<void> => {
                const { repository, id } = req.params
                const dump = await backend.dump(parseInt(id, 10))
                if (!dump || dump.repository !== decodeURIComponent(repository)) {
                    throw Object.assign(new Error('LSIF dump not found'), { status: 404 })
                }

                res.json(dump)
            }
        )
    )

    return router
}
