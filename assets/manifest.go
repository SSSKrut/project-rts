// Package assets loads baked models: the .glb the bake wrote plus the manifest
// that says what is in it. The manifest is the only reason this is possible —
// raylib hands back Model.Meshes as a bare array with no names, so which mesh
// is the turret at which LOD has to arrive as data.
//
// Nothing here feeds the sim. Hull sizes, collider radii and armour stay in
// VehicleSpecs; a model only decides what the player sees.
package assets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// ModelID indexes Registry.assets. Zero is a valid id, so absence is reported
// separately — a component that ever stores one keeps a numeric handle, never
// a string (save_spec allows plain data only).
type ModelID uint16

type vec3 [3]float32

func (v vec3) rl() rl.Vector3 { return rl.Vector3{X: v[0], Y: v[1], Z: v[2]} }

// MeshRef is one drawable: a mesh index into Model.Meshes plus the material
// bucket it belongs to. Glass is drawn after everything opaque.
type MeshRef struct {
	Mesh   int32  `json:"mesh"`
	Bucket string `json:"bucket"`
	Tris   int    `json:"tris"`
}

// Part is a rigid group with its own pivot. Turret yaw, gun elevation and wheel
// spin are runtime transforms about these, not baked animation. A part missing
// from a LOD has been folded into its parent for that level.
type Part struct {
	ID     string               `json:"id"`
	Parent string               `json:"parent"`
	Axis   string               `json:"axis"`
	Steer  bool                 `json:"steer"`
	Pivot  vec3                 `json:"pivot"`
	LODs   map[string][]MeshRef `json:"lods"`

	pivot  rl.Vector3
	byLOD  [][]MeshRef
	parent int
}

// Mount is a named attachment point in model space: exhaust, muzzle, smoke
// launcher, seat, boarding point. Pos and Dir are relative to the part named
// in Part, so a muzzle rides its turret.
type Mount struct {
	ID   string `json:"id"`
	Part string `json:"part"`
	Pos  vec3   `json:"pos"`
	Dir  vec3   `json:"dir"`
}

type lodRow struct {
	LOD   int `json:"lod"`
	Tris  int `json:"tris"`
	Draws int `json:"draws"`
}

// Asset is one baked model. Geometry is loaded on first use; everything else
// is available the moment the manifest is parsed, so a headless run can read
// mount positions without a GL context.
type Asset struct {
	Name       string   `json:"name"`
	Class      string   `json:"class"`
	Side       string   `json:"side"`
	Kind       string   `json:"kind"`
	Forward    string   `json:"forward"`
	ColorSpace string   `json:"vertex_color_space"`
	Size       vec3     `json:"size"`
	Parts      []Part   `json:"parts"`
	Mounts     []Mount  `json:"mounts"`
	LODs       []lodRow `json:"lods"`
	Bytes      int      `json:"bytes"`

	glb      string
	model    rl.Model
	loaded   bool
	partByID map[string]int
	mountsBy map[string]int
}

// LODCount is how many levels the bake produced.
func (a *Asset) LODCount() int { return len(a.LODs) }

// Part returns a part by id, or nil.
func (a *Asset) Part(id string) *Part {
	if i, ok := a.partByID[id]; ok {
		return &a.Parts[i]
	}
	return nil
}

// Mount returns an attachment point by id, or nil.
func (a *Asset) Mount(id string) *Mount {
	if i, ok := a.mountsBy[id]; ok {
		return &a.Mounts[i]
	}
	return nil
}

// Registry owns every asset and the one material they are drawn through.
type Registry struct {
	dir    string
	assets []*Asset
	byName map[string]ModelID

	shader      Shader
	material    rl.Material
	matReady    bool
	encodeSet   bool
	encodeKnown bool
	scratch     []rl.Matrix
}

// LoadRegistry parses <dir>/manifest.json. No GL is touched: geometry uploads
// on first draw, so scenes that never render (every ai_* gate) pay nothing and
// need no window.
func LoadRegistry(dir string) (*Registry, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var list []*Asset
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("assets: %s/manifest.json: %w", dir, err)
	}
	r := &Registry{dir: dir, byName: make(map[string]ModelID, len(list))}
	for _, a := range list {
		if err := a.prepare(dir); err != nil {
			return nil, err
		}
		r.byName[a.Name] = ModelID(len(r.assets))
		r.assets = append(r.assets, a)
	}
	return r, nil
}

func (a *Asset) prepare(dir string) error {
	a.glb = filepath.Join(dir, a.Name, a.Name+".glb")
	if _, err := os.Stat(a.glb); err != nil {
		return fmt.Errorf("assets: %s: %w", a.Name, err)
	}
	// The manifest lists parts alphabetically, so a child can precede its
	// parent. Order them by depth instead: partMatrices resolves in one pass
	// and would otherwise read an uninitialised parent matrix.
	depth := make(map[string]int, len(a.Parts))
	byID := make(map[string]*Part, len(a.Parts))
	for i := range a.Parts {
		byID[a.Parts[i].ID] = &a.Parts[i]
	}
	var depthOf func(id string, guard int) int
	depthOf = func(id string, guard int) int {
		if d, ok := depth[id]; ok {
			return d
		}
		p, ok := byID[id]
		if !ok || p.Parent == "" || p.Parent == id || guard > len(a.Parts) {
			depth[id] = 0
			return 0
		}
		d := depthOf(p.Parent, guard+1) + 1
		depth[id] = d
		return d
	}
	for i := range a.Parts {
		depthOf(a.Parts[i].ID, 0)
	}
	sort.SliceStable(a.Parts, func(i, j int) bool {
		return depth[a.Parts[i].ID] < depth[a.Parts[j].ID]
	})

	a.partByID = make(map[string]int, len(a.Parts))
	for i := range a.Parts {
		a.partByID[a.Parts[i].ID] = i
	}
	a.mountsBy = make(map[string]int, len(a.Mounts))
	for i := range a.Mounts {
		a.mountsBy[a.Mounts[i].ID] = i
	}
	for i := range a.Parts {
		p := &a.Parts[i]
		p.pivot = p.Pivot.rl()
		p.parent = -1
		if j, ok := a.partByID[p.Parent]; ok && p.Parent != p.ID {
			p.parent = j
		}
		p.byLOD = make([][]MeshRef, len(a.LODs))
		for key, refs := range p.LODs {
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(p.byLOD) {
				return fmt.Errorf("assets: %s part %s: bad LOD key %q", a.Name, p.ID, key)
			}
			p.byLOD[n] = refs
		}
	}
	return nil
}

// ID looks an asset up by its baked name.
func (r *Registry) ID(name string) (ModelID, bool) {
	id, ok := r.byName[name]
	return id, ok
}

// Names lists every asset, in manifest order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.assets))
	for _, a := range r.assets {
		out = append(out, a.Name)
	}
	return out
}

// Get returns the asset without touching the GPU.
func (r *Registry) Get(id ModelID) *Asset {
	if int(id) >= len(r.assets) {
		return nil
	}
	return r.assets[id]
}
