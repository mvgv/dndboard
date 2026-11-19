class_name Standee
extends Node3D
## A creature's miniature: a silhouette for its creature type on a base
## coloured by side, with a name and HP label. Only shows what the server sent.

const PC_COLOR := Color(0.85, 0.9, 1.0)
const PC_BASE := Color(0.2, 0.45, 0.9)
const MONSTER_COLOR := Color(0.12, 0.1, 0.1)
const MONSTER_BASE := Color(0.7, 0.15, 0.12)

var creature_id := ""
var square := Vector2i.ZERO

var _figure := Node3D.new()
var _label := Label3D.new()
var _name := ""


func setup(c: Dictionary) -> void:
	creature_id = str(c["id"])
	_name = str(c["name"])
	var pc := bool(c.get("player_character", false))

	var base := MeshInstance3D.new()
	var disc := CylinderMesh.new()
	disc.top_radius = 0.38
	disc.bottom_radius = 0.4
	disc.height = 0.06
	base.mesh = disc
	base.material_override = _material(PC_BASE if pc else MONSTER_BASE)
	base.position.y = 0.03
	_figure.add_child(base)

	var body := MeshInstance3D.new()
	body.mesh = Silhouettes.mesh(str(c.get("creature_type", "")))
	var mat := _material(PC_COLOR if pc else MONSTER_COLOR)
	mat.cull_mode = BaseMaterial3D.CULL_DISABLED
	mat.billboard_mode = BaseMaterial3D.BILLBOARD_FIXED_Y
	body.material_override = mat
	body.position.y = 0.06
	_figure.add_child(body)
	add_child(_figure)

	_label.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	_label.no_depth_test = true
	_label.pixel_size = 0.004
	_label.font_size = 40
	_label.outline_size = 10
	_label.position.y = 1.75
	add_child(_label)
	apply(c)


## Takes the server's view of the creature as the truth.
func apply(c: Dictionary) -> void:
	square = Vector2i(int(c["x"]), int(c["y"]))
	position = BoardView.square_center(square)
	var out := bool(c.get("down", false)) or bool(c.get("gone", false))
	visible = not bool(c.get("gone", false))
	_figure.rotation.x = -PI / 2.0 if out else 0.0
	_label.text = "%s\n%d/%d PV" % [_name, int(c["hp"]), int(c["max_hp"])]
	_label.modulate = Color(0.6, 0.6, 0.6) if out else Color.WHITE


func slide_to(to: Vector2i) -> void:
	square = to
	var tween := create_tween()
	tween.tween_property(self, "position", BoardView.square_center(to), 0.35)
	await tween.finished


func fall() -> void:
	var tween := create_tween()
	tween.tween_property(_figure, "rotation:x", -PI / 2.0, 0.4)
	await tween.finished


func head_position() -> Vector3:
	return global_position + Vector3(0, 1.4, 0)


static func _material(color: Color) -> StandardMaterial3D:
	var mat := StandardMaterial3D.new()
	mat.albedo_color = color
	return mat
