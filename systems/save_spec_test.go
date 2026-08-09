package systems

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mlange-42/ark/ecs"
)

func TestAssertPlainDataAcceptsPODShapes(t *testing.T) {
	type inner struct {
		A int32
		B float32
	}
	type outer struct {
		N   uint8
		Arr [4]inner
		F   [2][3]float64
		Ok  bool
	}
	if err := assertPlainData(reflect.TypeOf(outer{}), "outer"); err != nil {
		t.Fatalf("a plain struct was rejected: %v", err)
	}
	if err := assertPlainData(reflect.TypeOf(ecs.Entity{}), "entity"); err != nil {
		t.Fatalf("ecs.Entity was rejected: %v", err)
	}
}

func TestAssertPlainDataRejectsIndirection(t *testing.T) {
	type withString struct{ S string }
	type withSlice struct{ S []int }
	type withMap struct{ M map[string]int }
	type withPtr struct{ P *int }
	type withIface struct{ I any }
	type nestedString struct {
		Head  int
		Inner withString
	}
	type arrayOfSlices struct{ A [3][]byte }

	cases := []struct {
		name string
		v    any
	}{
		{"string", withString{}},
		{"slice", withSlice{}},
		{"map", withMap{}},
		{"pointer", withPtr{}},
		{"interface", withIface{}},
		{"nested string", nestedString{}},
		{"array of slices", arrayOfSlices{}},
	}
	for _, c := range cases {
		if err := assertPlainData(reflect.TypeOf(c.v), c.name); err == nil {
			t.Errorf("%s passed the plain-data check", c.name)
		}
	}
}

func TestAssertPlainDataNamesTheOffender(t *testing.T) {
	type withString struct{ S string }
	err := assertPlainData(reflect.TypeOf(withString{}), "components.Example")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "components.Example") {
		t.Errorf("error does not name the component: %v", err)
	}
}

func TestSavePolicyForRejectsUnknownComponents(t *testing.T) {
	type strayComponent struct{ X int }
	if _, err := savePolicyFor(reflect.TypeOf(strayComponent{})); err == nil {
		t.Fatal("an unregistered component got a policy instead of an error")
	}
}

// The permanent gate only walks components a live world happens to have
// registered. Registering every row first makes it a whole-table check: a
// memcpy policy on a type carrying a string or a slice fails here, not on a
// player's save.
func TestEveryMemcpyComponentIsPlainData(t *testing.T) {
	w := ecs.NewWorld()
	for name, entry := range saveComponents {
		if entry.LiveID == nil {
			t.Errorf("%s has no LiveID resolver", name)
			continue
		}
		entry.LiveID(w)
	}
	if err := VerifySaveSpec(w); err != nil {
		t.Fatalf("save spec is not self-consistent: %v", err)
	}
}

func TestSaveSpecRowsResolveToTheirOwnType(t *testing.T) {
	w := ecs.NewWorld()
	for name, entry := range saveComponents {
		id := entry.LiveID(w)
		info, ok := ecs.ComponentInfo(w, id)
		if !ok {
			t.Errorf("%s: no component info after registration", name)
			continue
		}
		if info.Type.String() != name {
			t.Errorf("table key %q resolves to type %s", name, info.Type)
		}
	}
}

func TestVerifySaveSpecFlagsAStrayComponent(t *testing.T) {
	type unregisteredComponent struct{ X int32 }
	w := ecs.NewWorld()
	ecs.ComponentID[unregisteredComponent](w)
	if err := VerifySaveSpec(w); err == nil {
		t.Fatal("a component with no save policy passed the gate")
	}
}
