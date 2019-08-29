import { createFilter, testFilter, encodeJSON, decodeJSON } from './encoding'

describe('Encoding', () => {
    describe('Bloom filters', () => {
        it('should test set membership', async () => {
            const filter = await createFilter(['foo', 'bar', 'baz'])
            expect(await testFilter(filter, 'foo')).toBeTruthy()
            expect(await testFilter(filter, 'bar')).toBeTruthy()
            expect(await testFilter(filter, 'baz')).toBeTruthy()
            expect(await testFilter(filter, 'bonk')).toBeFalsy()
            expect(await testFilter(filter, 'quux')).toBeFalsy()
        })
    })

    describe('JSON', () => {
        it('should preserve maps', async () => {
            const m = new Map<string, number>()
            m.set('a', 1)
            m.set('b', 2)
            m.set('c', 3)

            const value = {
                foo: [1, 2, 3],
                bar: ['abc', 'xyz'],
                baz: m,
            }

            const encoded = await encodeJSON(value)
            const decoded = await decodeJSON(encoded)
            expect(decoded).toEqual(value)
        })
    })
})
