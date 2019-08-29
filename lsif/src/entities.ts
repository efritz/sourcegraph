import { Id, MonikerKind } from 'lsif-protocol'
import * as lsp from 'vscode-languageserver-protocol'

/**
 * Data for a single document within an LSIF dump. The data here can answer definitions,
 * references, and hover queries if the results are all contained within the same document.
 */
export interface DocumentData {
    /**
     * A mapping from range ID to the index of the range in the `orderedRanges`
     * array.
     */
    ranges: Map<Id, number>

    /**
     * An array of range data sorted by startLine, then by startCharacter. This
     * allows us to perform binary search to find a particular location subsumed
     * by a range in the document.
     */
    orderedRanges: RangeData[]

    // A map of identifiers to a result set.
    resultSets: Map<Id, ResultSetData>

    // A map of identifiers to a definition result.
    definitionResults: Map<Id, DefinitionResultData>

    // A map of identifiers to a reference result.
    referenceResults: Map<Id, ReferenceResultData>

    // A map of identifiers to a hover result.
    hovers: Map<Id, HoverData>

    // A map of identifiers to a moniker.
    monikers: Map<Id, MonikerData>

    // A map of identifiers to package information.
    packageInformation: Map<Id, PackageInformationData>
}

/**
 * The set of fields shared by a range or a result set vertex. It contains
 * the same relevant edge data, which can be subsequently queried in the
 * containing document.
 */
interface ResultObjectData {
    /**
     * The set of moniker identifiers directly attached to this range or result
     * set. The moniker object can be queried by its identifier within the
     * containing document.
     */
    monikers: Id[]

    /**
     * The identifier of the hover result attached to this range or result set,
     * if one exists. The hover result object can be queried by its identifier
     * within the containing document.
     */
    hoverResult?: Id

    /**
     * The identifier of the definition result attached to this range or result
     * set, if one exists. The definition result object can be queried by its
     * identifier within the containing document.
     */
    definitionResult?: Id

    /**
     * The identifier of the reference result attached to this range or result
     * set, if one exists. The reference result object can be queried by its
     * identifier within the containing document.
     */
    referenceResult?: Id

    /**
     * The identifier of a result set attached to this range or result set, if one
     * exists. The result set object can be queried by its identifier within the
     * containing document.
     */
    next?: Id
}

/**
 * An internal representation of a range vertex from an LSIF dump. It contains the same
 * relevant edge data, which can be subsequently queried in the containing document.
 */
export interface RangeData extends ResultObjectData {
    // The start position of the range.
    start: lsp.Position

    // The end position of the range.
    end: lsp.Position
}

/**
 * An internal representation of a result set vertex from an LSIF dump. It contains the
 * same relevant edge data, which can be subsequently queried in the containing document.
 */
export interface ResultSetData extends ResultObjectData {}

/**
 * Data used to answer a definitions query.
 */
export interface DefinitionResultData {
    /**
     * A list of range identifiers that specify the definition. The range objects can be
     * queried by their identifier within the containing document.
     */
    values: Id[]
}

/**
 * Data used to answer a references query.
 */
export interface ReferenceResultData {
    // TODO - these can be collapsed, they're always merged in the API

    /**
     * A list of range identifiers that specify the definition of a target reference. The
     * range objects can be queried by their identifier within the containing document.
     */
    definitions: Id[]

    /**
     * A list of range identifiers that specify the references of a target definition. The
     * range objects can be queried by their identifier within the containing document.
     */
    references: Id[]
}

/**
 * Data used to answer a hover query.
 */
export interface HoverData {
    // TODO - normalize content
    // TODO - used MarkupContent, MarkedString is deprecated

    // The raw hover payload from the LSIf dump.
    contents: lsp.MarkupContent | lsp.MarkedString | lsp.MarkedString[]
}

/**
 * Data about a moniker attached to a range or a result set.
 */
export interface MonikerData {
    // The kind of moniker (e.g. local, import, export).
    kind: MonikerKind

    // The name of the package type (e.g. npm, pip).

    scheme: string

    // The unique identifier of the moniker.
    identifier: string

    /**
     * The identifier of the package information to this moniker, if one exists.
     * The package information object can be queried by its identifier within the
     * containing document.
     */
    packageInformation?: Id
}

/**
 * Additional data about a non-local moniker.
 */
export interface PackageInformationData {
    // The name of the package the moniker describes.
    name: string

    // The version of the package the moniker describes.
    version: string
}

/**
 * An LSP range that has been squashed into a single layer.
 */
export interface FlattenedRange {
    // The line on which the range starts (0-indexed, inclusive).
    startLine: number

    // The line on which the range ends (0-indexed, inclusive).
    startCharacter: number

    // The chracter on which the range starts (0-indexed, inclusive).
    endLine: number

    // The chracter on which the range ends (0-indexed, inclusive).
    endCharacter: number
}
