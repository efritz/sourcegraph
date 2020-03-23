import { Position, Range, Selection } from '@sourcegraph/extension-api-types'
import { WorkspaceRootWithMetadata } from '../api/client/services/workspaceService'
import { SearchPatternType } from '../graphql/schema'
import { FiltersToTypeAndValue } from '../search/interactive/util'
import { isEmpty } from 'lodash'
import { parseSearchQuery, CharacterRange } from '../search/parser/parser'
import { replaceRange } from './strings'

export interface RepoSpec {
    /**
     * The name of this repository on a Sourcegraph instance,
     * as affected by `repositoryPathPattern`.
     *
     * Example: `sourcegraph/sourcegraph`
     */
    repoName: string
}

export interface RawRepoSpec {
    /**
     * The name of this repository, unaffected by `repositoryPathPattern`.
     *
     * Example: `github.com/sourcegraph/sourcegraph`
     */
    rawRepoName: string
}

export interface RevSpec {
    /**
     * a revision string (like 'master' or 'my-branch' or '24fca303ac6da784b9e8269f724ddeb0b2eea5e7')
     */
    rev: string
}

export interface ResolvedRevSpec {
    /**
     * a 40 character commit SHA
     */
    commitID: string
}

export interface FileSpec {
    /**
     * a path to a directory or file
     */
    filePath: string
}

interface ComparisonSpec {
    /**
     * a diff specifier with optional base and comparison. Examples:
     * - "master..." (implicitly: "master...HEAD")
     * - "...my-branch" (implicitly: "HEAD...my-branch")
     * - "master...my-branch"
     */
    commitRange: string
}

/**
 * 1-indexed position in a blob.
 * Positions in URLs are 1-indexed.
 */
interface UIPosition {
    /** 1-indexed line number */
    line: number

    /** 1-indexed character number */
    character: number
}

/**
 * 1-indexed range in a blob.
 * Ranges in URLs are 1-indexed.
 */
interface UIRange {
    start: UIPosition
    end: UIPosition
}

export interface UIPositionSpec {
    /**
     * A 1-indexed point in the blob
     */
    position: UIPosition
}

interface UIRangeSpec {
    /**
     * A 1-indexed range in the blob
     */
    range: UIRange
}

/**
 * Specifies an LSP mode.
 */
export interface ModeSpec {
    /** The LSP mode, which identifies the language server to use. */
    mode: string
}

type BlobViewState = 'def' | 'references' | 'discussions' | 'impl'

export interface ViewStateSpec {
    /**
     * The view state (for the blob panel).
     */
    viewState: BlobViewState
}

/**
 * 'code' for Markdown/rich-HTML files rendered as code, 'rendered' for rendering them as
 * Markdown/rich-HTML, undefined for the default for the file type ('rendered' for Markdown, etc.,
 * 'code' otherwise).
 */
export type RenderMode = 'code' | 'rendered' | undefined

interface RenderModeSpec {
    /**
     * How the file should be rendered.
     */
    renderMode: RenderMode
}

/**
 * Properties of a RepoURI (like git://github.com/gorilla/mux#mux.go) or a URL (like https://sourcegraph.com/github.com/gorilla/mux/-/blob/mux.go)
 */
export interface ParsedRepoURI
    extends RepoSpec,
        Partial<RevSpec>,
        Partial<ResolvedRevSpec>,
        Partial<FileSpec>,
        Partial<ComparisonSpec>,
        Partial<UIPositionSpec>,
        Partial<UIRangeSpec> {}

/**
 * RepoURI is a URI identifing a repository resource, like
 *   - the repository itself: `git://github.com/gorilla/mux`
 *   - the repository at a particular revision: `git://github.com/gorilla/mux?rev`
 *   - a file in a repository at an immutable revision: `git://github.com/gorilla/mux?SHA#path/to/file.go
 *   - a line in a file in a repository at an immutable revision: `git://github.com/gorilla/mux?SHA#path/to/file.go:3
 *   - a character position in a file in a repository at an immutable revision: `git://github.com/gorilla/mux?SHA#path/to/file.go:3,5
 *   - a rangein a file in a repository at an immutable revision: `git://github.com/gorilla/mux?SHA#path/to/file.go:3,5-4,9
 */
type RepoURI = string

const parsePosition = (str: string): Position => {
    const split = str.split(',')
    if (split.length === 1) {
        return { line: parseInt(str, 10), character: 0 }
    }
    if (split.length === 2) {
        return { line: parseInt(split[0], 10), character: parseInt(split[1], 10) }
    }
    throw new Error('unexpected position: ' + str)
}

/**
 * Parses the properties of a legacy Git URI like git://github.com/gorilla/mux#mux.go.
 *
 * These URIs were used when communicating with language servers over LSP and with extensions. They are being
 * phased out in favor of URLs to resources in the Sourcegraph raw API, which do not require out-of-band
 * information to fetch the contents of.
 *
 * @deprecated Migrate to using URLs to the Sourcegraph raw API (or other concrete URLs) instead.
 */
export function parseRepoURI(uri: RepoURI): ParsedRepoURI {
    const parsed = new URL(uri)
    const repoName = parsed.hostname + parsed.pathname
    const rev = parsed.search.substr('?'.length) || undefined
    let commitID: string | undefined
    if (rev?.match(/[0-9a-fA-f]{40}/)) {
        commitID = rev
    }
    const fragmentSplit = parsed.hash.substr('#'.length).split(':').map(decodeURIComponent)
    let filePath: string | undefined
    let position: UIPosition | undefined
    let range: UIRange | undefined
    if (fragmentSplit.length === 1) {
        filePath = fragmentSplit[0]
    }
    if (fragmentSplit.length === 2) {
        filePath = fragmentSplit[0]
        const rangeOrPosition = fragmentSplit[1]
        const rangeOrPositionSplit = rangeOrPosition.split('-')

        if (rangeOrPositionSplit.length === 1) {
            position = parsePosition(rangeOrPositionSplit[0])
        }
        if (rangeOrPositionSplit.length === 2) {
            range = { start: parsePosition(rangeOrPositionSplit[0]), end: parsePosition(rangeOrPositionSplit[1]) }
        }
        if (rangeOrPositionSplit.length > 2) {
            throw new Error('unexpected range or position: ' + rangeOrPosition)
        }
    }
    if (fragmentSplit.length > 2) {
        throw new Error('unexpected fragment: ' + parsed.hash)
    }

    return { repoName, rev, commitID, filePath: filePath || undefined, position, range }
}

/**
 * A repo
 */
export interface Repo extends RepoSpec {}

/**
 * A repo with a (possibly unresolved) revspec.
 */
export interface RepoRev extends RepoSpec, RevSpec {}

/**
 * A repo resolved to an exact commit
 */
export interface AbsoluteRepo extends RepoSpec, RevSpec, ResolvedRevSpec {}

/**
 * A file in a repo
 */
export interface RepoFile extends RepoSpec, RevSpec, Partial<ResolvedRevSpec>, FileSpec {}

/**
 * A file at an exact commit
 */
export interface AbsoluteRepoFile extends RepoSpec, RevSpec, ResolvedRevSpec, FileSpec {}

/**
 * A position in file at an exact commit
 */
export interface AbsoluteRepoFilePosition
    extends RepoSpec,
        RevSpec,
        ResolvedRevSpec,
        FileSpec,
        UIPositionSpec,
        Partial<ViewStateSpec>,
        Partial<RenderModeSpec> {}

/**
 * Provide one.
 *
 * @param position either 1-indexed partial position
 * @param range or 1-indexed partial range spec
 */
export function toPositionOrRangeHash(ctx: {
    position?: { line: number; character?: number }
    range?: { start: { line: number; character?: number }; end: { line: number; character?: number } }
}): string {
    if (ctx.range) {
        const emptyRange =
            ctx.range.start.line === ctx.range.end.line && ctx.range.start.character === ctx.range.end.character
        return (
            '#L' +
            (emptyRange
                ? toPositionHashComponent(ctx.range.start)
                : `${toPositionHashComponent(ctx.range.start)}-${toPositionHashComponent(ctx.range.end)}`)
        )
    }
    if (ctx.position) {
        return '#L' + toPositionHashComponent(ctx.position)
    }
    return ''
}

/**
 * @param ctx 1-indexed partial position
 */
export function toPositionHashComponent(position: { line: number; character?: number }): string {
    return position.line.toString() + (position.character ? ':' + position.character : '')
}

/**
 * Represents a line, a position, a line range, or a position range. It forbids
 * just a character, or a range from a line to a position or vice versa (such as
 * "L1-2:3" or "L1:2-3"), none of which would make much sense.
 *
 * 1-indexed.
 */
export type LineOrPositionOrRange =
    | { line?: undefined; character?: undefined; endLine?: undefined; endCharacter?: undefined }
    | { line: number; character?: number; endLine?: undefined; endCharacter?: undefined }
    | { line: number; character?: undefined; endLine?: number; endCharacter?: undefined }
    | { line: number; character: number; endLine: number; endCharacter: number }

export function lprToRange(lpr: LineOrPositionOrRange): Range | undefined {
    if (lpr.line === undefined) {
        return undefined
    }
    return {
        start: { line: lpr.line, character: lpr.character || 0 },
        end: {
            line: lpr.endLine || lpr.line,
            character: lpr.endCharacter || lpr.character || 0,
        },
    }
}

export function lprToSelectionsZeroIndexed(lpr: LineOrPositionOrRange): Selection[] {
    const range = lprToRange(lpr)
    if (range === undefined) {
        return []
    }
    // `lprToRange` sets character to 0 if it's undefined. Only - 1 the character if it's not 0.
    const characterZeroIndexed = (character: number): number => (character === 0 ? character : character - 1)
    const start: Position = { line: range.start.line - 1, character: characterZeroIndexed(range.start.character) }
    const end: Position = { line: range.end.line - 1, character: characterZeroIndexed(range.end.character) }
    return [
        {
            start,
            end,
            anchor: start,
            active: end,
            isReversed: false,
        },
    ]
}

/**
 * Tells if the given fragment component is a legacy blob hash component or not.
 *
 * @param hash The URL fragment.
 */
export function isLegacyFragment(hash: string): boolean {
    if (hash.startsWith('#')) {
        hash = hash.substr('#'.length)
    }
    return (
        hash !== '' &&
        !hash.includes('=') &&
        (hash.includes('$info') ||
            hash.includes('$def') ||
            hash.includes('$references') ||
            hash.includes('$impl') ||
            hash.includes('$history'))
    )
}

/**
 * Parses the URL fragment (hash) portion, which consists of a line, position, or range in the file, plus an
 * optional "viewState" parameter (that encodes other view state, such as for the panel).
 *
 * For example, in the URL fragment "#L17:19-21:23$foo:bar", the "viewState" is "foo:bar".
 *
 * @template V The type that describes the view state (typically a union of string constants). There is no runtime
 *             check that the return value satisfies V.
 */
export function parseHash<V extends string>(hash: string): LineOrPositionOrRange & { viewState?: V } {
    if (hash.startsWith('#')) {
        hash = hash.substr('#'.length)
    }

    if (!isLegacyFragment(hash)) {
        // Modern hash parsing logic (e.g. for hashes like `"#L17:19-21:23&tab=foo:bar"`:
        const searchParams = new URLSearchParams(hash)
        const lpr = (findLineInSearchParams(searchParams) || {}) as LineOrPositionOrRange & {
            viewState?: V
        }
        if (searchParams.get('tab')) {
            lpr.viewState = searchParams.get('tab') as V
        }
        return lpr
    }

    // Legacy hash parsing logic (e.g. for hashes like "#L17:19-21:23$foo:bar" where the "viewState" is "foo:bar"):
    if (!/^(L[0-9]+(:[0-9]+)?(-[0-9]+(:[0-9]+)?)?)?(\$.*)?$/.test(hash)) {
        // invalid or empty hash
        return {}
    }
    const lineCharModalInfo = hash.split('$', 2) // e.g. "L17:19-21:23$references"
    const lpr = parseLineOrPositionOrRange(lineCharModalInfo[0]) as LineOrPositionOrRange & { viewState?: V }
    if (lineCharModalInfo[1]) {
        lpr.viewState = lineCharModalInfo[1] as V
    }
    return lpr
}

/**
 * Parses a string like "L1-2:3", a range from a line to a position.
 */
function parseLineOrPositionOrRange(lineChar: string): LineOrPositionOrRange {
    if (!/^(L[0-9]+(:[0-9]+)?(-L?[0-9]+(:[0-9]+)?)?)?$/.test(lineChar)) {
        return {} // invalid
    }

    // Parse the line or position range, ensuring we don't get an inconsistent result
    // (such as L1-2:3, a range from a line to a position).
    let line: number | undefined // 17
    let character: number | undefined // 19
    let endLine: number | undefined // 21
    let endCharacter: number | undefined // 23
    if (lineChar.startsWith('L')) {
        const posOrRangeString = lineChar.slice(1)
        const [startString, endString] = posOrRangeString.split('-', 2)
        if (startString) {
            const parsed = parseLineOrPosition(startString)
            line = parsed.line
            character = parsed.character
        }
        if (endString) {
            const parsed = parseLineOrPosition(endString)
            endLine = parsed.line
            endCharacter = parsed.character
        }
    }
    let lpr = { line, character, endLine, endCharacter } as LineOrPositionOrRange
    if (typeof line === 'undefined' || (typeof endLine !== 'undefined' && typeof character !== typeof endCharacter)) {
        lpr = {}
    } else if (typeof character === 'undefined') {
        lpr = typeof endLine === 'undefined' ? { line } : { line, endLine }
    } else if (typeof endLine === 'undefined' || typeof endCharacter === 'undefined') {
        lpr = { line, character }
    } else {
        lpr = { line, character, endLine, endCharacter }
    }
    return lpr
}

function toRenderModeQuery(ctx: Partial<RenderModeSpec>): string {
    if (ctx.renderMode === 'code') {
        return '?view=code'
    }
    return ''
}

/**
 * Finds the URL search parameter which has a key like "L1-2:3" without any
 * value.
 *
 * @param searchParams The URLSearchParams to look for the line in.
 */
function findLineInSearchParams(searchParams: URLSearchParams): LineOrPositionOrRange | undefined {
    for (const key of searchParams.keys()) {
        if (key.startsWith('L')) {
            return parseLineOrPositionOrRange(key)
        }
        break
    }
    return undefined
}

function parseLineOrPosition(
    str: string
): { line: undefined; character: undefined } | { line: number; character?: number } {
    if (str.startsWith('L')) {
        str = str.slice(1)
    }
    const parts = str.split(':', 2)
    let line: number | undefined
    let character: number | undefined
    if (parts.length >= 1) {
        line = parseInt(parts[0], 10)
    }
    if (parts.length === 2) {
        character = parseInt(parts[1], 10)
    }
    line = typeof line === 'number' && isNaN(line) ? undefined : line
    character = typeof character === 'number' && isNaN(character) ? undefined : character
    if (typeof line === 'undefined') {
        return { line: undefined, character: undefined }
    }
    return { line, character }
}

/** Encodes a repository at a revspec for use in a URL. */
export function encodeRepoRev(repo: string, rev?: string): string {
    return rev ? `${repo}@${escapeRevspecForURL(rev)}` : repo
}

export function toPrettyBlobURL(
    ctx: RepoFile & Partial<UIPositionSpec> & Partial<ViewStateSpec> & Partial<UIRangeSpec> & Partial<RenderModeSpec>
): string {
    return `/${encodeRepoRev(ctx.repoName, ctx.rev)}/-/blob/${ctx.filePath}${toRenderModeQuery(
        ctx
    )}${toPositionOrRangeHash(ctx)}${toViewStateHashComponent(ctx.viewState)}`
}

/**
 * Encodes rev with encodeURIComponent, except that slashes ('/') are preserved,
 * because they are not ambiguous in any of the current places where used, and URLs
 * for (e.g.) branches with slashes look a lot nicer with '/' than '%2F'.
 */
export function escapeRevspecForURL(rev: string): string {
    return encodeURIComponent(rev).replace(/%2F/g, '/')
}

export function toViewStateHashComponent(viewState: string | undefined): string {
    return viewState ? `&tab=${viewState}` : ''
}

const positionStr = (pos: Position): string => pos.line + '' + (pos.character ? ',' + pos.character : '')

/**
 * The inverse of parseRepoURI, this generates a string from parsed values.
 */
export function makeRepoURI(parsed: ParsedRepoURI): RepoURI {
    const rev = parsed.commitID || parsed.rev
    let uri = `git://${parsed.repoName}`
    uri += rev ? '?' + rev : ''
    uri += parsed.filePath ? '#' + parsed.filePath : ''
    uri += parsed.position || parsed.range ? ':' : ''
    uri += parsed.position ? positionStr(parsed.position) : ''
    uri += parsed.range ? positionStr(parsed.range.start) + '-' + positionStr(parsed.range.end) : ''
    return uri
}

export const toRootURI = (ctx: RepoSpec & ResolvedRevSpec): string => `git://${ctx.repoName}?${ctx.commitID}`
export function toURIWithPath(ctx: RepoSpec & ResolvedRevSpec & FileSpec): string {
    return `git://${ctx.repoName}?${ctx.commitID}#${ctx.filePath}`
}

/**
 * Translate a URI to use the input revision (e.g., branch names) instead of the Git commit SHA if the URI is
 * inside of a workspace root. This helper is used to translate URLs (from actions such as go-to-definition) to
 * avoid navigating the user from (e.g.) a URL with a nice Git branch name to a URL with a full Git commit SHA.
 *
 * For example, suppose there is a workspace root `git://r?a9cb9d` with input revision `mybranch`. If {@link uri}
 * is `git://r?a9cb9d#f`, it would be translated to `git://r?mybranch#f`.
 */
export function withWorkspaceRootInputRevision(
    workspaceRoots: readonly WorkspaceRootWithMetadata[],
    uri: ParsedRepoURI
): ParsedRepoURI {
    const inWorkspaceRoot = workspaceRoots.find(root => {
        const rootURI = parseRepoURI(root.uri)
        return rootURI.repoName === uri.repoName && rootURI.rev === uri.rev
    })
    if (inWorkspaceRoot?.inputRevision !== undefined) {
        return { ...uri, commitID: undefined, rev: inWorkspaceRoot.inputRevision }
    }
    return uri // unchanged
}

/**
 * Builds a URL query for the given query (without leading `?`).
 *
 * @param query the search query
 * @param patternType the pattern type this query should be interpreted in.
 * Having a `patternType:` filter in the query overrides this argument.
 * @param filtersInQuery filters in an interactive mode query. For callers of
 * this function requiring correct behavior in interactive mode, this param
 * must be passed.
 *
 */
export function buildSearchURLQuery(
    query: string,
    patternType: SearchPatternType,
    caseSensitive: boolean,
    filtersInQuery?: FiltersToTypeAndValue
): string {
    const searchParams = new URLSearchParams()
    let fullQuery = query

    if (filtersInQuery && !isEmpty(filtersInQuery)) {
        fullQuery = [fullQuery, generateFiltersQuery(filtersInQuery)].filter(query => query.length > 0).join(' ')
    }

    const patternTypeInQuery = parsePatternTypeFromQuery(fullQuery)
    if (patternTypeInQuery) {
        fullQuery = replaceRange(fullQuery, patternTypeInQuery.range)
        searchParams.set('q', fullQuery)
        searchParams.set('patternType', patternTypeInQuery.value)
    } else {
        searchParams.set('q', fullQuery)
        searchParams.set('patternType', patternType)
    }

    const caseInQuery = parseCaseSensitivityFromQuery(fullQuery)
    if (caseInQuery) {
        fullQuery = replaceRange(fullQuery, caseInQuery.range)
        searchParams.set('q', fullQuery)

        if (caseInQuery.value === 'yes') {
            fullQuery = replaceRange(fullQuery, caseInQuery.range)
            searchParams.set('case', caseInQuery.value)
        } else {
            // For now, remove case when case:no, since it's the default behavior. Avoids
            // queries breaking when only `repo:` filters are specified.
            //
            // TODO: just set case=no when https://github.com/sourcegraph/sourcegraph/issues/7671 is fixed.
            searchParams.delete('case')
        }
    } else {
        searchParams.set('q', fullQuery)
        if (caseSensitive) {
            searchParams.set('case', 'yes')
        } else {
            // For now, remove case when case:no, since it's the default behavior. Avoids
            // queries breaking when only `repo:` filters are specified.
            //
            // TODO: just set case=no when https://github.com/sourcegraph/sourcegraph/issues/7671 is fixed.
            searchParams.delete('case')
        }
    }

    // eslint-disable-next-line @typescript-eslint/no-base-to-string
    return searchParams.toString().replace(/%2F/g, '/').replace(/%3A/g, ':')
}

/**
 * Creates the raw string representation of the filters currently in the query in interactive mode.
 *
 * @param filtersInQuery the map representing the filters currently in an interactive mode query.
 */
export function generateFiltersQuery(filtersInQuery: FiltersToTypeAndValue): string {
    const fieldKeys = Object.keys(filtersInQuery)
    return fieldKeys
        .filter(key => filtersInQuery[key].value.trim().length > 0)
        .map(key => `${filtersInQuery[key].negated ? '-' : ''}${filtersInQuery[key].type}:${filtersInQuery[key].value}`)
        .join(' ')
}

export function parsePatternTypeFromQuery(query: string): { range: CharacterRange; value: string } | undefined {
    const parsedQuery = parseSearchQuery(query)
    if (parsedQuery.type === 'success') {
        for (const member of parsedQuery.token.members) {
            const token = member.token
            if (
                token.type === 'filter' &&
                token.filterType.token.value.toLowerCase() === 'patterntype' &&
                token.filterValue
            ) {
                return {
                    range: { start: token.filterType.range.start, end: token.filterValue.range.end },
                    value: query.substring(token.filterValue.range.start, token.filterValue.range.end),
                }
            }
        }
    }

    return undefined
}

export function parseCaseSensitivityFromQuery(query: string): { range: CharacterRange; value: string } | undefined {
    const parsedQuery = parseSearchQuery(query)
    if (parsedQuery.type === 'success') {
        for (const member of parsedQuery.token.members) {
            const token = member.token
            if (token.type === 'filter' && token.filterType.token.value.toLowerCase() === 'case' && token.filterValue) {
                return {
                    range: { start: token.filterType.range.start, end: token.filterValue.range.end },
                    value: query.substring(token.filterValue.range.start, token.filterValue.range.end),
                }
            }
        }
    }
    return undefined
}
