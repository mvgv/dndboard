class_name DiceTray
extends Node3D
## Cosmetic dice. The server has already decided every result: a die tumbles
## and then shows the face the server sent. Nothing here is random in effect.

const ROLL_TIME := 0.9
const HOLD_TIME := 0.7
const DIE_COLOR := Color(0.95, 0.93, 0.85)
const D20_COLOR := Color(0.85, 0.2, 0.2)

var _meshes := {}


## Tumbles one die per face onto the spot, then shows the faces.
func roll(faces: Array, sides: int, at: Vector3) -> void:
	var dice: Array[Node3D] = []
	var n := faces.size()
	for i in n:
		var die := _make_die(sides)
		var land := at + Vector3((i - (n - 1) / 2.0) * 0.55, 0.35, 0.0)
		die.position = land + Vector3(randf_range(-0.6, 0.6), 2.5, randf_range(0.4, 1.0))
		add_child(die)
		dice.append(die)
		var tween := die.create_tween().set_parallel()
		tween.tween_property(die, "position", land, ROLL_TIME).set_trans(Tween.TRANS_BOUNCE).set_ease(Tween.EASE_OUT)
		tween.tween_property(die, "rotation", Vector3(randf_range(6, 14), randf_range(6, 14), randf_range(6, 14)), ROLL_TIME).set_ease(Tween.EASE_OUT)
	await get_tree().create_timer(ROLL_TIME).timeout
	for i in n:
		var face := Label3D.new()
		face.text = str(int(faces[i]))
		face.billboard = BaseMaterial3D.BILLBOARD_ENABLED
		face.no_depth_test = true
		face.font_size = 72
		face.outline_size = 14
		face.pixel_size = 0.004
		face.position = dice[i].position + Vector3(0, 0.45, 0)
		add_child(face)
		dice.append(face)
	await get_tree().create_timer(HOLD_TIME).timeout
	for d in dice:
		d.queue_free()


## Floats a short text (a result) up from a spot and fades it; does not block.
func float_text(text: String, at: Vector3, color := Color.WHITE) -> void:
	var label := Label3D.new()
	label.text = text
	label.modulate = color
	label.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	label.no_depth_test = true
	label.font_size = 56
	label.outline_size = 12
	label.pixel_size = 0.004
	label.position = at
	add_child(label)
	var tween := label.create_tween().set_parallel()
	tween.tween_property(label, "position", at + Vector3(0, 0.8, 0), 1.6)
	tween.tween_property(label, "modulate:a", 0.0, 1.6).set_delay(0.6)
	tween.chain().tween_callback(label.queue_free)


func _make_die(sides: int) -> MeshInstance3D:
	var die := MeshInstance3D.new()
	if not _meshes.has(sides):
		_meshes[sides] = _die_mesh(sides)
	die.mesh = _meshes[sides]
	var mat := StandardMaterial3D.new()
	mat.albedo_color = D20_COLOR if sides == 20 else DIE_COLOR
	mat.cull_mode = BaseMaterial3D.CULL_DISABLED
	die.material_override = mat
	return die


static func _die_mesh(sides: int) -> Mesh:
	match sides:
		6:
			var box := BoxMesh.new()
			box.size = Vector3.ONE * 0.4
			return box
		8:
			return _polyhedron(
				[Vector3.UP, Vector3.DOWN, Vector3.LEFT, Vector3.RIGHT, Vector3.FORWARD, Vector3.BACK],
				[[0, 3, 4], [0, 4, 2], [0, 2, 5], [0, 5, 3], [1, 4, 3], [1, 2, 4], [1, 5, 2], [1, 3, 5]],
				0.28)
	# d20, and the stand-in for every other die for now.
	var t := (1.0 + sqrt(5.0)) / 2.0
	return _polyhedron(
		[Vector3(-1, t, 0), Vector3(1, t, 0), Vector3(-1, -t, 0), Vector3(1, -t, 0),
		Vector3(0, -1, t), Vector3(0, 1, t), Vector3(0, -1, -t), Vector3(0, 1, -t),
		Vector3(t, 0, -1), Vector3(t, 0, 1), Vector3(-t, 0, -1), Vector3(-t, 0, 1)],
		[[0, 11, 5], [0, 5, 1], [0, 1, 7], [0, 7, 10], [0, 10, 11],
		[1, 5, 9], [5, 11, 4], [11, 10, 2], [10, 7, 6], [7, 1, 8],
		[3, 9, 4], [3, 4, 2], [3, 2, 6], [3, 6, 8], [3, 8, 9],
		[4, 9, 5], [2, 4, 11], [6, 2, 10], [8, 6, 7], [9, 8, 1]],
		0.25 / t)


## A flat-shaded polyhedron from unit-ish vertices and faces.
static func _polyhedron(verts: Array, faces: Array, scale: float) -> ArrayMesh:
	var st := SurfaceTool.new()
	st.begin(Mesh.PRIMITIVE_TRIANGLES)
	for f in faces:
		var a: Vector3 = verts[f[0]] * scale
		var b: Vector3 = verts[f[1]] * scale
		var c: Vector3 = verts[f[2]] * scale
		var n := (c - a).cross(b - a).normalized()
		if n.dot(a + b + c) < 0.0:
			n = -n
			var swap := b
			b = c
			c = swap
		st.set_normal(n)
		st.add_vertex(a)
		st.add_vertex(c)
		st.add_vertex(b)
	return st.commit()
