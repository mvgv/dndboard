# dndboard

## Play the combat demo

Start the server (two Fighters against three Goblin Warriors on a hand-built Board):

    go run ./server/cmd/dndboard            # ws://127.0.0.1:8080/play

Then start one client per Player with Godot 4.7:

    godot --path client -- --server=ws://127.0.0.1:8080/play

Every connected client may act for the Fighter whose turn it is; the server plays the goblins. Click a blue square to move, a red square to attack, and use the buttons for the other actions. Q and E turn the camera, and the mouse wheel zooms.
