import * as lsp from 'vscode-languageserver';
import { Database, UriTransformer } from './database';
import { DocumentInfo } from './files';
import { Id } from 'lsif-protocol';
import { URI } from 'vscode-uri';

export class NamedDatabase {
  constructor(
    public name: string,
    public enabled: boolean,
    public ext: string,
    public db: Database
  ) { }
}

export class MultiDatabase extends Database {
  constructor(private databases: Array<NamedDatabase>) {
    super()
  }

  public load(file: string, transformerFactory: (projectRoot: string) => UriTransformer): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const promises = Array<Promise<NamedDatabase | null>>();
      for (const db of this.databases) {
        if (!db.enabled) {
          continue
        }

        promises.push(instrumentPromise(
          `${db.name}: load`,
          db.db.load(file + db.ext, transformerFactory).then(_ => db).catch(e => {
            if ('code' in e && e.code === 'ENOENT') {
              return null
            }

            throw e
          })
        ))
      }

      Promise.all(promises).then(dbs => {
        // Remove all the databases that failed to load due to a file in the
        // storage root either being purge or not existing in the first place
        // due to a feature flag being disabled.
        this.databases = <Array<NamedDatabase>>dbs.filter(db => !!db)

        // If we didn't load anything, raise an ENOENT to signal the server
        // and cache that we don't have any data to use for this guy.
        if (this.databases.length === 0) {
          reject(Object.assign(new Error('No databases loaded'), { code: 'ENOENT' }))
          return
        }

        resolve()
      })
    }).then(_ => {
      this.initialize(transformerFactory)
    })
  }

  public close(): void {
    this.databases.map(d => d.db.close())
  }

  // These methods are teh ones called in the http-server path. These should
  // be instrumented so that we know what performs better than the others.

  public hover(uri: string, position: lsp.Position): lsp.Hover | undefined {
    return this.compareResults(
      'hover',
      this.databases.map(db => instrument(`${db.name}: hover`, () => db.db.hover(uri, position)))
    )
  }

  public definitions(uri: string, position: lsp.Position): lsp.Location | lsp.Location[] | undefined {
    return this.compareResults(
      'definitions',
      this.databases.map(db => instrument(`${db.name}: definitions`, () => db.db.definitions(uri, position)))
    )
  }

  public references(uri: string, position: lsp.Position, context: lsp.ReferenceContext): lsp.Location[] | undefined {
    return this.compareResults(
      'references',
      this.databases.map(db => instrument(`${db.name}: references`, () => db.db.references(uri, position, context)))
    )
  }

  // Define the remaining methods, although they are not called *directly* in
  // the http server path. These methods are called indirectly from the child
  // class in inheritance hierarchy.

  public getProjectRoot(): URI {
    return this.databases.map(db => db.db.getProjectRoot())[0]
  }

  public foldingRanges(uri: string): lsp.FoldingRange[] | undefined {
    return this.databases.map(db => db.db.foldingRanges(uri))[0]
  }

  public documentSymbols(uri: string): lsp.DocumentSymbol[] | undefined {
    return this.databases.map(db => db.db.documentSymbols(uri))[0]
  }

  public declarations(uri: string, position: lsp.Position): lsp.Location | lsp.Location[] | undefined {
    return this.databases.map(db => db.db.declarations(uri, position))[0]
  }

  // NOTE: Originally these

  public getDocumentInfos(): DocumentInfo[] {
    return this.databases.map(db => db.db.getDocumentInfos())[0]
  }

  public findFile(uri: string): Id | undefined {
    return this.databases.map(db => db.db.findFile(uri))[0]
  }

  public fileContent(id: Id): string | undefined {
    return this.databases.map(db => db.db.fileContent(id))[0]
  }

  private compareResults<T>(name: string, results: Array<T>): T {
    for (let i = 1; i < results.length; i++) {
      if (results[i] !== results[0]) {
        console.warn(`Unexpected differing result for ${name} between: ${this.databases[0].name} and ${this.databases[i].name}`, results[0], 'and', results[i])
      }
    }

    return results[0]
  }
}

//
// Helpers

export function instrument<T>(name: string, f: () => T): T {
  const start = new Date().getTime()
  const res = f();
  const elapsed = new Date().getTime() - start
  console.log(`${name} completed in ${elapsed}ms`)
  return res;
}

export function instrumentPromise<T>(name: string, p: Promise<T>): Promise<T> {
  const start = new Date().getTime()
  return p.then(res => {
    const elapsed = new Date().getTime() - start
    console.log(`${name} completed in ${elapsed}ms`)
    return res;
  });
}
