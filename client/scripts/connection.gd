class_name Connection
extends Node
## WebSocket link to the game server. Receives JSON messages and sends
## intents; it knows nothing about the rules (ADR 0004).

signal opened
signal closed(reason: String)
signal message_received(message: Dictionary)

var _peer := WebSocketPeer.new()
var _state := WebSocketPeer.STATE_CLOSED


func connect_to(url: String) -> Error:
	_peer = WebSocketPeer.new()
	_peer.inbound_buffer_size = 1 << 20
	var err := _peer.connect_to_url(url)
	if err == OK:
		_state = WebSocketPeer.STATE_CONNECTING
	return err


func is_open() -> bool:
	return _state == WebSocketPeer.STATE_OPEN


func send_intent(intent: Dictionary) -> void:
	if not is_open():
		return
	var msg := intent.duplicate()
	msg["type"] = "intent"
	_peer.send_text(JSON.stringify(msg))


func _process(_delta: float) -> void:
	if _state == WebSocketPeer.STATE_CLOSED:
		return
	_peer.poll()
	var now := _peer.get_ready_state()
	if now == WebSocketPeer.STATE_OPEN and _state != WebSocketPeer.STATE_OPEN:
		opened.emit()
	_state = now
	if now == WebSocketPeer.STATE_OPEN:
		while _peer.get_available_packet_count() > 0:
			var text := _peer.get_packet().get_string_from_utf8()
			var parsed: Variant = JSON.parse_string(text)
			if parsed is Dictionary:
				message_received.emit(parsed)
			else:
				push_warning("ignoring a malformed server message")
	elif now == WebSocketPeer.STATE_CLOSED:
		var reason := _peer.get_close_reason()
		closed.emit(reason if reason != "" else "connection closed")
