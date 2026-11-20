# dndboard

## Play the combat demo

Start the server (two Fighters against three Goblin Warriors on a hand-built Board; the goblins are played by the tactical brain):

    go run ./server/cmd/dndboard            # ws://127.0.0.1:8080/play

Then start one client per Fighter with Godot 4.7. Each client plays the Player Character named in its address and sees only what that character perceives:

    godot --path client -- --server='ws://127.0.0.1:8080/play?pc=fighter-1'
    godot --path client -- --server='ws://127.0.0.1:8080/play?pc=fighter-2'

The game waits on a Fighter's turn until its client acts. Click a blue square to move, a red square to attack, a green square to stabilize a dying ally, and use the buttons for the other actions. Q and E turn the camera, and the mouse wheel zooms.
