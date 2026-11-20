class_name Hud
extends CanvasLayer
## Screen overlay: connection, whose turn it is, the action buttons the
## server's legal actions allow, and a plain-text log. UI text is PT-BR;
## rules terms are provisional until the terminology table (#23).

signal connect_requested(url: String)
signal action_pressed(kind: String)

var _url := LineEdit.new()
var _connect := Button.new()
var _connect_box := HBoxContainer.new()
var _status := Label.new()
var _turn := Label.new()
var _hint := Label.new()
var _log := RichTextLabel.new()
var _buttons := {}


func _ready() -> void:
	var root := MarginContainer.new()
	root.set_anchors_preset(Control.PRESET_FULL_RECT)
	root.mouse_filter = Control.MOUSE_FILTER_IGNORE
	for side in ["left", "right", "top", "bottom"]:
		root.add_theme_constant_override("margin_" + side, 12)
	add_child(root)

	var top := VBoxContainer.new()
	top.mouse_filter = Control.MOUSE_FILTER_IGNORE
	top.size_flags_vertical = Control.SIZE_SHRINK_BEGIN
	root.add_child(top)
	_url.custom_minimum_size.x = 320
	_connect.text = "Conectar"
	_connect.pressed.connect(func() -> void: connect_requested.emit(_url.text.strip_edges()))
	_connect_box.add_child(_url)
	_connect_box.add_child(_connect)
	top.add_child(_connect_box)
	top.add_child(_status)
	_turn.add_theme_font_size_override("font_size", 22)
	top.add_child(_turn)
	top.add_child(_hint)

	_log.bbcode_enabled = false # server text is shown verbatim, never parsed
	_log.scroll_following = true
	_log.custom_minimum_size = Vector2(380, 260)
	_log.size_flags_horizontal = Control.SIZE_SHRINK_END
	_log.size_flags_vertical = Control.SIZE_SHRINK_BEGIN
	_log.mouse_filter = Control.MOUSE_FILTER_PASS
	root.add_child(_log)

	var bar := HBoxContainer.new()
	bar.size_flags_horizontal = Control.SIZE_SHRINK_CENTER
	bar.size_flags_vertical = Control.SIZE_SHRINK_END
	bar.add_theme_constant_override("separation", 8)
	root.add_child(bar)
	for pair in [["dash", "Disparada"], ["disengage", "Desengajar"], ["stand", "Levantar"],
			["nimble_escape", "Fuga Ágil (bônus)"], ["end_turn", "Encerrar turno"]]:
		var b := Button.new()
		b.text = pair[1]
		var kind: String = pair[0]
		b.pressed.connect(func() -> void: action_pressed.emit(kind))
		b.disabled = true
		bar.add_child(b)
		_buttons[kind] = b


func set_url(url: String) -> void:
	_url.text = url


func set_connected(connected: bool) -> void:
	_connect_box.visible = not connected


func set_status(text: String) -> void:
	_status.text = text


func set_turn(text: String) -> void:
	_turn.text = text


func set_hint(text: String) -> void:
	_hint.text = text


## Enables exactly the buttons the legal actions allow; none without them.
func set_legal(legal: Dictionary) -> void:
	for kind: String in _buttons:
		_buttons[kind].disabled = not bool(legal.get(kind, false))


func add_log(line: String) -> void:
	_log.add_text(line + "\n")
