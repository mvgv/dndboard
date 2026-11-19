extends Node3D
## Wires the client together. It holds no rules logic (ADR 0004): it renders
## the server's state, highlights the server's legal actions, plays the
## server's events (dice included) and sends back the intent the Player picks.
##
## Run with `godot --path client -- --server=ws://host:port/play` to connect
## on start; otherwise enter the address in the HUD.

const DEFAULT_URL := "ws://127.0.0.1:8080/play"
const NO_SQUARE := Vector2i(-1, -1)

var _conn := Connection.new()
var _board := BoardView.new()
var _dice := DiceTray.new()
var _hud := Hud.new()
var _camera := Camera3D.new()
var _pivot := Node3D.new()
var _camera_distance := 10.0

var _standees := {} # creature id -> Standee
var _names := {} # creature id -> display name
var _queue: Array[Dictionary] = []
var _busy := false
var _waiting := false # an intent is out; wait for its update or rejection

# The active Player Character's legal actions, straight from the server.
var _legal := {}
var _moves := {} # Vector2i -> movement cost in feet
var _targets := {} # Vector2i -> target creature id


func _ready() -> void:
	_build_scene()
	_conn.opened.connect(func() -> void:
		_hud.set_connected(true)
		_hud.set_status("Conectado"))
	_conn.closed.connect(func(reason: String) -> void:
		_hud.set_connected(false)
		_hud.set_status("Desconectado: " + reason)
		_set_legal({}))
	_conn.message_received.connect(_on_message)
	_hud.connect_requested.connect(_connect_to)
	_hud.action_pressed.connect(func(kind: String) -> void: _send({"intent": kind}))

	var url := DEFAULT_URL
	var autoconnect := false
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--server="):
			url = arg.trim_prefix("--server=")
			autoconnect = true
	_hud.set_url(url)
	if autoconnect:
		_connect_to(url)


func _build_scene() -> void:
	add_child(_conn)
	add_child(_board)
	add_child(_dice)
	add_child(_hud)

	var env := WorldEnvironment.new()
	env.environment = Environment.new()
	env.environment.background_mode = Environment.BG_COLOR
	env.environment.background_color = Color(0.08, 0.08, 0.1)
	env.environment.ambient_light_source = Environment.AMBIENT_SOURCE_COLOR
	env.environment.ambient_light_color = Color(0.5, 0.5, 0.55)
	add_child(env)
	var sun := DirectionalLight3D.new()
	sun.rotation_degrees = Vector3(-55, -30, 0)
	sun.shadow_enabled = true
	add_child(sun)

	add_child(_pivot)
	_pivot.add_child(_camera)
	_place_camera(10.0)


func _place_camera(distance: float) -> void:
	_camera_distance = clampf(distance, 4.0, 30.0)
	_camera.position = Vector3(0, _camera_distance * 0.85, _camera_distance * 0.6)
	_camera.look_at_from_position(_camera.position, Vector3.ZERO)


func _connect_to(url: String) -> void:
	_hud.set_status("Conectando a %s…" % url)
	if _conn.connect_to(url) != OK:
		_hud.set_status("Endereço inválido: " + url)


# --- Messages ------------------------------------------------------------

func _on_message(m: Dictionary) -> void:
	_queue.append(m)
	if not _busy:
		_drain()


## Plays messages one at a time so animations never overlap or reorder.
func _drain() -> void:
	_busy = true
	while not _queue.is_empty():
		await _handle(_queue.pop_front())
	_busy = false


func _handle(m: Dictionary) -> void:
	match str(m.get("type", "")):
		"welcome":
			_welcome(m)
		"update":
			_waiting = false
			_set_legal({})
			for e: Dictionary in m.get("events", []):
				await _play(e)
			_apply(m)
		"rejected":
			_waiting = false
			_hud.add_log("Recusado: %s" % str(m.get("reason", "")))
			_set_legal(_legal)


func _welcome(m: Dictionary) -> void:
	var board: Dictionary = m["board"]
	_board.build(board["rows"])
	_pivot.position = Vector3(float(board["width"]) / 2.0, 0, float(board["height"]) / 2.0)
	for s: Standee in _standees.values():
		s.queue_free()
	_standees.clear()
	_names.clear()
	for e: Dictionary in m.get("events", []):
		if e.get("type") == "initiative_rolled" and not e.get("hidden", false):
			_hud.add_log("Iniciativa de %s: %d" % [str(e["creature"]), int(e["total"])])
	_apply(m)


## Takes the server's state and legal actions as the truth.
func _apply(m: Dictionary) -> void:
	var state: Dictionary = m["state"]
	for c: Dictionary in state["creatures"]:
		var id := str(c["id"])
		_names[id] = str(c["name"])
		if not _standees.has(id):
			var s := Standee.new()
			add_child(s)
			s.setup(c)
			_standees[id] = s
		else:
			_standees[id].apply(c)
	var pending: Dictionary = state.get("pending", {})
	if not pending.is_empty():
		# Answering Reaction Prompts from the client comes with #16.
		_hud.set_turn("Aguardando a reação de %s (%d s)" % [_name(str(pending["creature"])), int(pending["remaining_s"])])
	elif bool(state.get("over", false)):
		_hud.set_turn("Combate encerrado: vitória de %s" % _team_name(str(state.get("winner", ""))))
	else:
		var who := _name(str(state["active"]))
		var whose := "" if m.has("legal") else " (monstro)"
		_hud.set_turn("Rodada %d: vez de %s%s" % [int(state["round"]), who, whose])
	_set_legal(m.get("legal", {}))


func _set_legal(legal: Dictionary) -> void:
	_legal = legal
	_moves.clear()
	_targets.clear()
	var move_squares: Array[Vector2i] = []
	var target_squares: Array[Vector2i] = []
	for mv: Dictionary in legal.get("moves", []):
		var sq := Vector2i(int(mv["x"]), int(mv["y"]))
		_moves[sq] = int(mv["cost_ft"])
		move_squares.append(sq)
	for a: Dictionary in legal.get("attacks", []):
		var id := str(a["target"])
		if _standees.has(id):
			var sq: Vector2i = _standees[id].square
			_targets[sq] = id
			target_squares.append(sq)
	_board.show_highlights(move_squares, target_squares)
	_hud.set_legal(legal)
	if legal.is_empty():
		_hud.set_hint("")
	else:
		_hud.set_hint("Movimento restante: %d pés" % int(legal.get("movement_left_ft", 0)))


# --- Events --------------------------------------------------------------

func _play(e: Dictionary) -> void:
	match str(e.get("type", "")):
		"turn_started":
			_hud.add_log("— Rodada %d: vez de %s" % [int(e["round"]), _name(str(e["creature"]))])
		"moved":
			var to: Dictionary = e["to"]
			var s: Standee = _standees.get(str(e["creature"]))
			if s:
				await s.slide_to(Vector2i(int(to["x"]), int(to["y"])))
		"attack_rolled":
			await _attack(e)
		"damage_dealt":
			await _damage(e)
		"creature_downed":
			_hud.add_log("%s cai!" % _name(str(e["creature"])))
			var s: Standee = _standees.get(str(e["creature"]))
			if s:
				await s.fall()
		"creature_left":
			_hud.add_log("%s deixa o combate." % _name(str(e["creature"])))
		"dashed":
			_hud.add_log("%s usa Disparada (+%d pés)." % [_name(str(e["creature"])), int(e["extra_ft"])])
		"disengaged":
			_hud.add_log("%s usa Desengajar." % _name(str(e["creature"])))
		"opportunity_attack":
			_hud.add_log("%s faz um Ataque de Oportunidade contra %s!" % [_name(str(e["attacker"])), _name(str(e["target"]))])
		"reaction_prompted":
			_hud.add_log("%s pode fazer um Ataque de Oportunidade contra %s." % [_name(str(e["creature"])), _name(str(e["target"]))])
		"reaction_declined":
			_hud.add_log("%s não reage." % _name(str(e["creature"])))
		"reaction_timed_out":
			_hud.add_log("%s não respondeu a tempo: sem reação." % _name(str(e["creature"])))
		"combat_ended":
			_hud.add_log("Fim do combate: vitória de %s." % _team_name(str(e["winner"])))


func _attack(e: Dictionary) -> void:
	var attacker := _name(str(e["attacker"]))
	var target := _name(str(e["target"]))
	var hit := bool(e.get("hit", false))
	var crit := bool(e.get("critical", false))
	var outcome := "Crítico!" if crit else ("Acerto" if hit else "Erro")
	var at := _head(str(e["target"]))
	if e.get("hidden", false):
		# Behind the screen: only the result, no dice.
		_hud.add_log("%s ataca %s com %s: %s" % [attacker, target, str(e["weapon"]), outcome])
		_dice.float_text(outcome, at, _outcome_color(hit))
		await get_tree().create_timer(0.6).timeout
		return
	await _dice.roll([int(e["d20"])], 20, _spot(str(e["attacker"])))
	_hud.add_log("%s ataca %s com %s: d20 %d + %d = %d contra CA %d: %s" % [
		attacker, target, str(e["weapon"]), int(e["d20"]), int(e["modifier"]),
		int(e["total"]), int(e["ac"]), outcome])
	_dice.float_text(outcome, at, _outcome_color(hit))


func _damage(e: Dictionary) -> void:
	var id := str(e["target"])
	var amount := int(e["amount"])
	var line := "%s sofre %d de dano %s (PV %d)" % [_name(id), amount, str(e.get("damage_type", "")), int(e["hp_after"])]
	if not e.get("hidden", false):
		var rolls: Array = e.get("rolls", [])
		await _dice.roll(rolls, int(e.get("dice_sides", 6)), _spot(id))
		line += ": dados %s + %d" % [", ".join(rolls.map(func(r: Variant) -> String: return str(int(r)))), int(e["bonus"])]
	_hud.add_log(line)
	_dice.float_text("-%d" % amount, _head(id), Color(1, 0.4, 0.3))
	await get_tree().create_timer(0.3).timeout


# --- Input ---------------------------------------------------------------

func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventMouseMotion:
		var sq := _square_under((event as InputEventMouseMotion).position)
		_board.show_hover(sq if (_moves.has(sq) or _targets.has(sq)) else NO_SQUARE)
		if _targets.has(sq):
			_hud.set_hint("Atacar %s" % _name(_targets[sq]))
		elif _moves.has(sq):
			_hud.set_hint("Mover: %d pés" % _moves[sq])
	elif event is InputEventMouseButton and event.pressed:
		var mb := event as InputEventMouseButton
		match mb.button_index:
			MOUSE_BUTTON_LEFT:
				_click(_square_under(mb.position))
			MOUSE_BUTTON_WHEEL_UP:
				_place_camera(_camera_distance * 0.9)
			MOUSE_BUTTON_WHEEL_DOWN:
				_place_camera(_camera_distance * 1.1)
	elif event is InputEventKey and event.pressed:
		match (event as InputEventKey).keycode:
			KEY_Q:
				_pivot.rotate_y(deg_to_rad(15))
			KEY_E:
				_pivot.rotate_y(deg_to_rad(-15))


func _click(sq: Vector2i) -> void:
	if _busy or _legal.is_empty():
		return
	if _targets.has(sq):
		_send({"intent": "attack", "target": _targets[sq]})
	elif _moves.has(sq):
		_send({"intent": "move", "to": {"x": sq.x, "y": sq.y}})


## Sends an intent for the active Player Character, if it has legal actions.
func _send(intent: Dictionary) -> void:
	if _waiting or _legal.is_empty():
		return
	intent["actor"] = str(_legal["creature"])
	_waiting = true
	_board.clear_highlights()
	_conn.send_intent(intent)


func _square_under(screen: Vector2) -> Vector2i:
	return _board.square_at(_camera.project_ray_origin(screen), _camera.project_ray_normal(screen))


# --- Helpers -------------------------------------------------------------

func _name(id: String) -> String:
	return str(_names.get(id, id))


func _team_name(team: String) -> String:
	match team:
		"party":
			return "o grupo"
		"monsters":
			return "os monstros"
	return team


func _head(id: String) -> Vector3:
	var s: Standee = _standees.get(id)
	return s.head_position() if s else _pivot.position


## Where a creature's dice land: just in front of it, toward the camera.
func _spot(id: String) -> Vector3:
	var s: Standee = _standees.get(id)
	var base := s.global_position if s else _pivot.position
	return base + _pivot.global_basis.z * 0.8


func _outcome_color(hit: bool) -> Color:
	return Color(0.4, 1.0, 0.4) if hit else Color(0.8, 0.8, 0.8)
