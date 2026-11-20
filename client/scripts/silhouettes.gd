class_name Silhouettes
extends RefCounted
## Generic standee silhouettes per creature type, as flat outlines (x across,
## y up, in squares). Custom creature art is out of scope for the MVP.

const HUMANOID := [
	Vector2(-0.22, 0.0), Vector2(-0.12, 0.0), Vector2(-0.08, 0.55), Vector2(0.08, 0.55),
	Vector2(0.12, 0.0), Vector2(0.22, 0.0), Vector2(0.15, 0.62), Vector2(0.24, 1.05),
	Vector2(0.3, 0.7), Vector2(0.36, 0.72), Vector2(0.28, 1.15), Vector2(0.1, 1.2),
	Vector2(0.12, 1.42), Vector2(0.0, 1.5), Vector2(-0.12, 1.42), Vector2(-0.1, 1.2),
	Vector2(-0.28, 1.15), Vector2(-0.36, 0.72), Vector2(-0.3, 0.7), Vector2(-0.24, 1.05),
	Vector2(-0.15, 0.62),
]

## Small, big-headed, pointed ears.
const FEY := [
	Vector2(-0.18, 0.0), Vector2(-0.08, 0.0), Vector2(-0.05, 0.35), Vector2(0.05, 0.35),
	Vector2(0.08, 0.0), Vector2(0.18, 0.0), Vector2(0.12, 0.42), Vector2(0.2, 0.7),
	Vector2(0.3, 0.45), Vector2(0.34, 0.48), Vector2(0.24, 0.8), Vector2(0.1, 0.84),
	Vector2(0.16, 0.95), Vector2(0.36, 1.05), Vector2(0.15, 1.05), Vector2(0.0, 1.15),
	Vector2(-0.15, 1.05), Vector2(-0.36, 1.05), Vector2(-0.16, 0.95), Vector2(-0.1, 0.84),
	Vector2(-0.24, 0.8), Vector2(-0.34, 0.48), Vector2(-0.3, 0.45), Vector2(-0.2, 0.7),
	Vector2(-0.12, 0.42),
]

## Four legs and a long body.
const BEAST := [
	Vector2(-0.42, 0.0), Vector2(-0.34, 0.0), Vector2(-0.3, 0.3), Vector2(0.22, 0.3),
	Vector2(0.26, 0.0), Vector2(0.34, 0.0), Vector2(0.34, 0.45), Vector2(0.42, 0.62),
	Vector2(0.5, 0.6), Vector2(0.48, 0.75), Vector2(0.34, 0.78), Vector2(0.24, 0.62),
	Vector2(-0.34, 0.62), Vector2(-0.46, 0.74), Vector2(-0.44, 0.55), Vector2(-0.42, 0.45),
]

## Hunched and ragged.
const UNDEAD := [
	Vector2(-0.24, 0.0), Vector2(-0.1, 0.12), Vector2(0.02, 0.0), Vector2(0.12, 0.12),
	Vector2(0.24, 0.0), Vector2(0.2, 0.7), Vector2(0.4, 0.95), Vector2(0.34, 1.0),
	Vector2(0.16, 0.95), Vector2(0.14, 1.22), Vector2(0.02, 1.3), Vector2(-0.1, 1.2),
	Vector2(-0.3, 1.0), Vector2(-0.42, 0.62), Vector2(-0.34, 0.6), Vector2(-0.22, 0.82),
]

## Anything without a silhouette of its own yet.
const GENERIC := [
	Vector2(-0.3, 0.0), Vector2(0.3, 0.0), Vector2(0.3, 0.9), Vector2(0.18, 1.15),
	Vector2(0.0, 1.22), Vector2(-0.18, 1.15), Vector2(-0.3, 0.9),
]


static func outline(creature_type: String) -> PackedVector2Array:
	match creature_type.to_lower():
		"humanoid":
			return PackedVector2Array(HUMANOID)
		"fey":
			return PackedVector2Array(FEY)
		"beast", "monstrosity":
			return PackedVector2Array(BEAST)
		"undead":
			return PackedVector2Array(UNDEAD)
	return PackedVector2Array(GENERIC)


## A flat, double-sided mesh of the outline, standing on y = 0.
static func mesh(creature_type: String) -> ArrayMesh:
	var points := outline(creature_type)
	var indices := Geometry2D.triangulate_polygon(points)
	var st := SurfaceTool.new()
	st.begin(Mesh.PRIMITIVE_TRIANGLES)
	st.set_normal(Vector3.BACK)
	for p in points:
		st.add_vertex(Vector3(p.x, p.y, 0.0))
	for i in indices:
		st.add_index(i)
	return st.commit()
