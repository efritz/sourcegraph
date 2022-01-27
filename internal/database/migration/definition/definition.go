package definition

import (
	"sort"

	"github.com/cockroachdb/errors"
	"github.com/keegancsmith/sqlf"
)

type Definition struct {
	ID           int
	UpFilename   string
	UpQuery      *sqlf.Query
	DownFilename string
	DownQuery    *sqlf.Query
	Parents      []int
}

type IndexMetadata struct {
	TableName string
	IndexName string
}

type Definitions struct {
	definitions    []Definition
	definitionsMap map[int]Definition
}

func newDefinitions(migrationDefinitions []Definition) *Definitions {
	definitionsMap := make(map[int]Definition, len(migrationDefinitions))
	for _, migrationDefinition := range migrationDefinitions {
		definitionsMap[migrationDefinition.ID] = migrationDefinition
	}

	return &Definitions{
		definitions:    migrationDefinitions,
		definitionsMap: definitionsMap,
	}
}

// All returns the set of all definitions ordered such that each migration occurs
// only after all of its parents. The returned slice is not a copy, so it is not
// meant to be mutated.
func (ds *Definitions) All() []Definition {
	return ds.definitions
}

func (ds *Definitions) Count() int {
	return len(ds.definitions)
}

func (ds *Definitions) First() int {
	return ds.definitions[0].ID
}

func (ds *Definitions) GetByID(id int) (Definition, bool) {
	definition, ok := ds.definitionsMap[id]
	return definition, ok
}

// Root returns the definition with no parents.
func (ds *Definitions) Root() Definition {
	return ds.definitions[0]
}

// Leaves returns the definitions with no children.
func (ds *Definitions) Leaves() []Definition {
	childrenMap := children(ds.definitions)

	leaves := make([]Definition, 0, 4)
	for _, definition := range ds.definitions {
		if len(childrenMap[definition.ID]) == 0 {
			leaves = append(leaves, definition)
		}
	}

	return leaves
}

// Filter returns a new definitions object that contains the intersection of the
// receiver's definitions and the given identifiers. This operation is designed to
// cut complete branches of migrations from the tree (for use in squash operations).
// Therefore, it is an error for any of the remaining migrations to reference a
// parent that was not included in the target set of migrations.
func (ds *Definitions) Filter(ids []int) (*Definitions, error) {
	idMap := map[int]struct{}{}
	for _, id := range ids {
		idMap[id] = struct{}{}
	}

	filtered := make([]Definition, 0, len(ds.definitions)-len(ids))
	for _, definition := range ds.definitions {
		if _, ok := idMap[definition.ID]; ok {
			filtered = append(filtered, definition)
		}
	}

	for _, definition := range filtered {
		for _, parent := range definition.Parents {
			if _, ok := idMap[parent]; !ok {
				return nil, fmt.Errorf("illegal filter: migration %d (included) references parent migration %d (excluded)", definition.ID, parent)
			}
		}
	}

	return newDefinitions(filtered), nil
}

// LeafDominator returns the unique migration definition that dominates the set
// of leaf migrations. If no such migration exists, a false-valued flag is returned.
//
// Note that if there is a single leaf, this function returns that leaf. If there
// exist multiple leaves, then this function returns the nearest common ancestor (nca)
// of all leaves. This gives us a nice clean single-entry, single-exit graph prefix
// that can be squashed into a single migration.
//
//              +-- ... --+           +-- [ leaf 1 ]
//              |         |           |
//    [ root ] -+         +- [ nca ] -+
//              |         |           |
//              +-- ... --+           +-- [ leaf 2 ]
func (ds *Definitions) LeafDominator() (Definition, bool) {
	leaves := ds.Leaves()
	if len(leaves) == 0 {
		return Definition{}, false
	}

	dominators := ds.dominators()

	ids := make([][]int, 0, len(leaves))
	for _, leaf := range leaves {
		ids = append(ids, dominators[leaf.ID])
	}

	same := intersect(ids[0], ids[1:]...)
	if len(same) == 0 {
		return Definition{}, false
	}

	// Choose deepest common dominating migration
	return ds.GetByID(same[0])
}

// dominators solves the following dataflow equation for each migration definition.
//
// dom(n) = { n } union (intersect dom(p) over { p | preds(n) })
//
// This function returns a map from migration identifiers to the set of identifiers
// of dominating migrations. Because migrations are acyclic, we can solve this equation
// with a single pass over the graph rather than needing to iterate until fixed point.
//
// Note that due to traversal order, the set of dominators will be inversely ordered by
// depth.
func (ds *Definitions) dominators() map[int][]int {
	dominators := map[int][]int{}
	for _, definition := range ds.definitions {
		ds := []int{definition.ID}

		if len(definition.Parents) != 0 {
			a := dominators[definition.Parents[0]]
			bs := make([][]int, 0, len(definition.Parents))
			for _, parent := range definition.Parents[1:] {
				bs = append(bs, dominators[parent])
			}

			ds = append(ds, intersect(a, bs...)...)
		}

		dominators[definition.ID] = ds
	}

	return dominators
}

// intersect returns the intersection of all given sets. The elements of the output slice will
// have the same order as the first input slice.
func intersect(a []int, bs ...[]int) []int {
	intersection := make([]int, len(a))
	copy(intersection, a)

	for _, b := range bs {
		bMap := make(map[int]struct{}, len(b))
		for _, v := range b {
			bMap[v] = struct{}{}
		}

		filtered := intersection[:0]
		for _, v := range intersection {
			if _, ok := bMap[v]; ok {
				filtered = append(filtered, v)
			}
		}

		intersection = filtered
	}

	return intersection
}

func (ds *Definitions) UpTo(id, target int) ([]Definition, error) {
	if target == 0 {
		return ds.UpFrom(id, 0)
	}

	if _, ok := ds.GetByID(target); !ok {
		return nil, errors.Newf("unknown target %d", target)
	}
	if target < id {
		return nil, errors.Newf("migration %d is behind version %d", target, id)
	}
	if target == id {
		// n == 0 has special meaning; handle case immediately
		return nil, nil
	}

	return ds.UpFrom(id, target-id)
}

func (ds *Definitions) UpFrom(id, n int) ([]Definition, error) {
	slice := make([]Definition, 0, len(ds.definitions))
	for _, definition := range ds.definitions {
		if definition.ID <= id {
			continue
		}

		slice = append(slice, definition)
	}

	if n > 0 && len(slice) > n {
		slice = slice[:n]
	}

	if id != 0 && len(slice) != 0 && slice[0].ID != id+1 {
		return nil, errors.Newf("missing migrations [%d, %d]", id+1, slice[0].ID-1)
	}

	return slice, nil
}

func (ds *Definitions) DownTo(id, target int) ([]Definition, error) {
	if target == 0 {
		return nil, errors.Newf("illegal downgrade target %d", target)
	}

	if _, ok := ds.GetByID(target); !ok {
		return nil, errors.Newf("unknown target %d", target)
	}
	if id < target {
		return nil, errors.Newf("migration %d is ahead of version %d", target, id)
	}

	return ds.DownFrom(id, id-target)
}

func (ds *Definitions) DownFrom(id, n int) ([]Definition, error) {
	slice := make([]Definition, 0, len(ds.definitions))
	for _, definition := range ds.definitions {
		if definition.ID <= id {
			slice = append(slice, definition)
		}
	}

	sort.Slice(slice, func(i, j int) bool {
		return slice[j].ID < slice[i].ID
	})

	if len(slice) > n {
		slice = slice[:n]
	}

	if id != 0 && len(slice) != 0 && slice[0].ID != id {
		return nil, errors.Newf("missing migrations [%d, %d]", slice[0].ID+1, id)
	}

	return slice, nil
}
