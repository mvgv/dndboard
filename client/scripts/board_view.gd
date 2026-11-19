class_name BoardView
extends Node3D
## The Board in 3D: one unit per 5-foot square, square (x, y) centred at
## (x + 0.5, 0, y + 0.5). Draws terrain from the server's rows and highlights
## the squares the server lists; it never computes reachability itself.

const FLOOR_COLOR := Color(0.55, 0.5, 0.42)
const DIFFICULT_COLOR := Color(0.42, 0.47, 0.33)
const WALL_COLOR := Color(0.3, 0.28, 0.3)
const MOVE_COLOR := Color(0.25, 0.6, 1.0, 0.45)
const TARGET_COLOR := Color(1.0, 0.25, 0.2, 0.55)
const HOVER_COLOR := Color(1.0, 1.0, 1.0, 0.35)

var width := 0
var height := 0

var _highlights := Node3D.new()
var _hover := MeshInstance3D.new()


func _ready() -> void:
	add_child(_highlights)
	_hover.mesh = _square_mesh(0.96)
	_hover.material_override = _flat(HOVER_COLOR, true)
	_hover.position.y = 0.03
	_hover.visible = false
	add_child(_hover)


## Builds the terrain from text rows: '.' floor, '#' wall, '~' Difficult Terrain.
func build(rows: Array) -> void:
	for child in get_children():
		if child != _highlights and child != _hover:
			child.queue_free()
	height = rows.size()
	width = 0 if rows.is_empty() else str(rows[0]).length()
	var floor_mat := _flat(FLOOR_COLOR)
	var difficult_mat := _flat(DIFFICULT_COLOR)
	var wall_mat := _flat(WALL_COLOR)
	var tile := BoxMesh.new()
	tile.size = Vector3(0.98, 0.1, 0.98)
	var wall := BoxMesh.new()
	wall.size = Vector3(1.0, 1.2, 1.0)
	for y in height:
		var row := str(rows[y])
		for x in row.length():
			var m := MeshInstance3D.new()
			match row[x]:
				"#":
					m.mesh = wall
					m.material_override = wall_mat
					m.position = Vector3(x + 0.5, 0.6, y + 0.5)
				"~":
					m.mesh = tile
					m.material_override = difficult_mat
					m.position = Vector3(x + 0.5, -0.05, y + 0.5)
				_:
					m.mesh = tile
					m.material_override = floor_mat
					m.position = Vector3(x + 0.5, -0.05, y + 0.5)
			add_child(m)


static func square_center(square: Vector2i) -> Vector3:
	return Vector3(square.x + 0.5, 0.0, square.y + 0.5)


## Returns the square under a camera ray, or (-1, -1) off the Board.
func square_at(ray_origin: Vector3, ray_dir: Vector3) -> Vector2i:
	var hit: Variant = Plane(Vector3.UP, 0.0).intersects_ray(ray_origin, ray_dir)
	if hit == null:
		return Vector2i(-1, -1)
	var p: Vector3 = hit
	var sq := Vector2i(floori(p.x), floori(p.z))
	if sq.x < 0 or sq.y < 0 or sq.x >= width or sq.y >= height:
		return Vector2i(-1, -1)
	return sq


## Highlights reachable squares and the squares of valid targets.
func show_highlights(moves: Array[Vector2i], targets: Array[Vector2i]) -> void:
	clear_highlights()
	var move_mat := _flat(MOVE_COLOR, true)
	var target_mat := _flat(TARGET_COLOR, true)
	var mesh := _square_mesh(0.9)
	for sq in moves:
		_add_highlight(sq, mesh, move_mat)
	for sq in targets:
		_add_highlight(sq, mesh, target_mat)


func clear_highlights() -> void:
	for child in _highlights.get_children():
		child.queue_free()


func show_hover(square: Vector2i) -> void:
	_hover.visible = square.x >= 0
	if _hover.visible:
		_hover.position = square_center(square) + Vector3(0, 0.03, 0)


func _add_highlight(sq: Vector2i, mesh: Mesh, mat: Material) -> void:
	var m := MeshInstance3D.new()
	m.mesh = mesh
	m.material_override = mat
	m.position = square_center(sq) + Vector3(0, 0.02, 0)
	_highlights.add_child(m)


static func _square_mesh(size: float) -> PlaneMesh:
	var p := PlaneMesh.new()
	p.size = Vector2(size, size)
	return p


static func _flat(color: Color, transparent := false) -> StandardMaterial3D:
	var mat := StandardMaterial3D.new()
	mat.albedo_color = color
	if transparent:
		mat.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
		mat.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	return mat
