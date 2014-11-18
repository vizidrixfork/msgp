package parse

import (
	"github.com/philhofer/msgp/gen"
)

// findUnresolved finds identifiers and attempts
// to match them with other known (custom) identifiers.
// any unrecognized identifiers left over after a second
// pass are returned.
func findUnresolved(g gen.Elem) []string {

	switch g.Type() {
	case gen.PtrType:
		return findUnresolved(g.(*gen.Ptr).Value)

	case gen.SliceType:
		return findUnresolved(g.(*gen.Slice).Els)

	case gen.BaseType:
		b := g.(*gen.BaseElem)
		if b.Value == gen.IDENT { // type is unrecognized
			id := b.Ident
			if tp, ok := globalIdents[id]; ok {

				// skip types that the code generator has seen
				_, ok = globalProcessed[id]
				if ok {
					return nil
				}

				// if we have found another identity
				if tp != gen.IDENT {
					// Lower type one level
					i := b.Ident
					*b = gen.BaseElem{
						Value:   tp,   // "true" type
						Ident:   i,    // identifier name
						Convert: true, // requires explicit conversion
					}
					return nil
				}
			}
			return []string{b.Ident}
		}
		return nil

	case gen.StructType:
		s := g.(*gen.Struct)

		out := make([]string, 0, len(s.Fields))
		nm := s.Name
		_, ok := globalIdents[nm]

		// we have to check that the name is
		// not empty (b/c of anonymous embedded structs)
		if !ok && nm != "" {
			out = append(out, nm)
		}

		for _, field := range s.Fields {
			out = append(out, findUnresolved(field.FieldElem)...)
		}
		return out

	case gen.MapType:
		return findUnresolved(g.(*gen.Map).Value)

	default:
		return nil
	}
}
