#!/usr/bin/env python3
"""Writes maps/city.json: a 7x7 street grid, ~300 buildings in styled blocks,
a river with bridges through the west side and dirt tracks out of town.
Deterministic — the same seed gives the same city."""
import json
import random
from pathlib import Path

rng = random.Random(20260912)
PITCH = 80
STREETS = [-240 + i * PITCH for i in range(7)]
SETBACK = 9          # building edge to street centreline
MARGIN = 2           # between footprints
RIVER_CLEAR = 18     # footprint to river centreline

# --- roads -------------------------------------------------------------------
nodes, idx, edges = [], {}, []
for z in STREETS:
    for x in STREETS:
        idx[(x, z)] = len(nodes)
        nodes.append([x, z])
for z in STREETS:
    for i in range(len(STREETS) - 1):
        kind = "highway" if z == 0 else "local"
        edges.append({"from": idx[(STREETS[i], z)], "to": idx[(STREETS[i + 1], z)],
                      "kind": kind, "width": 4 if kind == "highway" else 3})
for x in STREETS:
    for j in range(len(STREETS) - 1):
        kind = "highway" if x == 0 else "local"
        edges.append({"from": idx[(x, STREETS[j])], "to": idx[(x, STREETS[j + 1])],
                      "kind": kind, "width": 4 if kind == "highway" else 3})
def outbound(frm, to, kind="dirt", width=2.5):
    nodes.append(list(to))
    edges.append({"from": idx[frm], "to": len(nodes) - 1, "kind": kind, "width": width})
outbound((240, 0), (335, 0), "highway", 4)
outbound((-240, 0), (-335, 0), "highway", 4)
outbound((0, 240), (30, 335))
outbound((0, -240), (-40, -335))
outbound((240, 160), (330, 230))
outbound((-240, -160), (-330, -230))

# --- river (west side, north-south, meandering between two street lines) -----
river = [[-200 + 14 * ((i % 3) - 1), -340 + i * 68] for i in range(11)]
def river_x(z):
    for (x0, z0), (x1, z1) in zip(river, river[1:]):
        if z0 <= z <= z1:
            t = (z - z0) / (z1 - z0)
            return x0 + (x1 - x0) * t
    return river[0][0] if z < river[0][1] else river[-1][0]

# --- buildings ---------------------------------------------------------------
placed = []   # (minx, minz, maxx, maxz)
buildings = []

def free(minx, minz, maxx, maxz):
    for (ax, az, bx, bz) in placed:
        if minx < bx + MARGIN and maxx > ax - MARGIN and minz < bz + MARGIN and maxz > az - MARGIN:
            return False
    for z in (minz, maxz, (minz + maxz) / 2):
        rx = river_x(z)
        if minx - RIVER_CLEAR < rx < maxx + RIVER_CLEAR:
            return False
    return True

def put(template, cx, cz, sx, sz, **extra):
    minx, minz, maxx, maxz = cx - sx / 2, cz - sz / 2, cx + sx / 2, cz + sz / 2
    if not free(minx, minz, maxx, maxz):
        return False
    placed.append((minx, minz, maxx, maxz))
    b = {"template": template, "seed": rng.randrange(1, 1 << 16),
         "x": round(cx, 1), "z": round(cz, 1)}
    b.update(extra)
    buildings.append(b)
    return True

def pick_kind():
    r = rng.random()
    if r < 0.55:
        sx, sz = rng.choice([8, 10, 12, 14]), rng.choice([8, 10])
        return "house", sx, sz, {"stories": rng.choice([1, 1, 2, 2, 3]), "sizeX": sx, "sizeZ": sz}
    if r < 0.85:
        return "office", 14, 10, {}
    sx, sz = rng.choice([18, 22]), rng.choice([16, 18])
    return "courtyard", sx, sz, {"stories": rng.choice([2, 3]), "sizeX": sx, "sizeZ": sz}

# door faces the street the building backs onto: 0=south(+Z) 1=east 2=north(-Z) 3=west
def ring(x0, z0, x1, z1, sides):
    ix0, iz0, ix1, iz1 = x0 + SETBACK, z0 + SETBACK, x1 - SETBACK, z1 - SETBACK
    for side in sides:
        cur = 0.0
        span = (ix1 - ix0) if side in ("n", "s") else (iz1 - iz0)
        while True:
            kind, sx, sz, extra = pick_kind()
            along, depth = (sx, sz) if side in ("n", "s") else (sz, sx)
            if cur + along > span:
                break
            if side == "n":
                cx, cz, door = ix0 + cur + along / 2, iz0 + depth / 2, 2
            elif side == "s":
                cx, cz, door = ix0 + cur + along / 2, iz1 - depth / 2, 0
            elif side == "w":
                cx, cz, door = ix0 + depth / 2, iz0 + cur + along / 2, 3
            else:
                cx, cz, door = ix1 - depth / 2, iz0 + cur + along / 2, 1
            if kind == "house":
                extra["doorSide"] = door
            if kind == "office" and side in ("e", "w"):
                sx, sz = sz, sx   # office footprint is fixed 14x10; keep its long side along the street
            put(kind, cx, cz, along, depth, **extra)
            cur += along + rng.uniform(3, 7)

def block(x0, z0, x1, z1):
    cx, cz = (x0 + x1) / 2, (z0 + z1) / 2
    style = rng.choices(["dense", "office", "park", "compound"], [50, 20, 12, 18])[0]
    if style == "dense":
        ring(x0, z0, x1, z1, ["n", "s", "e", "w"])
        if rng.random() < 0.5:
            put("courtyard", cx, cz, 22, 18, stories=2, sizeX=22, sizeZ=18)
    elif style == "office":
        ring(x0, z0, x1, z1, ["n", "s"])
        put("office", cx - 12, cz, 14, 10)
        put("office", cx + 12, cz, 14, 10)
    elif style == "park":
        put("house", cx, cz, 10, 8, stories=1, sizeX=10, sizeZ=8, doorSide=4)
    else:
        put("compound_plus", cx, cz, 30, 30)
        ring(x0, z0, x1, z1, [rng.choice(["n", "s", "e", "w"])])

for j in range(len(STREETS) - 1):
    for i in range(len(STREETS) - 1):
        block(STREETS[i], STREETS[j], STREETS[i + 1], STREETS[j + 1])

# outskirts along the tracks
for (x, z) in [(300, 40), (300, -35), (-300, 30), (-305, -40), (40, 300), (-20, -300),
               (290, 210), (-290, -210), (255, 290), (-260, 300)]:
    put("compound", x, z, 24, 24)
for (x, z) in [(275, 25), (-280, -25), (15, 285), (-10, -280), (310, 260), (-310, -255)]:
    put("house", x, z, 10, 8, stories=1, sizeX=10, sizeZ=8, doorSide=4)

city = {
    "name": "city",
    "terrain": {"seed": 5005, "wavelengthM": 170, "octaves": 3, "lacunarity": 2.0,
                "persistence": 0.45, "amplitudeM": 2.5},
    "buildings": buildings,
    "roads": {"nodes": nodes, "edges": edges},
    "trenches": [],
    "rivers": [{"points": river, "width": 8, "depth": 1.5}],
}
out = Path(__file__).resolve().parent.parent / "maps" / "city.json"
out.write_text(json.dumps(city, indent=1))
from collections import Counter
print(f"{out.name}: {len(buildings)} buildings {dict(Counter(b['template'] for b in buildings))}, "
      f"{len(nodes)} road nodes, {len(edges)} edges")
