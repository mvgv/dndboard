# dndboard

## Play the combat demo

Start the server (two Fighters against three Goblin Warriors on a hand-built Board; the goblins are played by the tactical brain):

    go run ./server/cmd/dndboard            # http://127.0.0.1:8080; add -dev for solo Campaigns

Everything needs a login. There is no lobby screen in the client yet, so set up the Campaign with curl (the token goes in an `Authorization: Bearer` header):

    curl -d '{"name":"ana","password":"correct horse"}' localhost:8080/register
    curl -d '{"name":"ana","password":"correct horse"}' localhost:8080/login            # {"token":"…"}
    curl -H "Authorization: Bearer $TOKEN" -d '{"adventure":"arena"}' localhost:8080/campaigns   # id and invite code
    curl -H "Authorization: Bearer $TOKEN" -d '{"invite":"CODE"}' localhost:8080/campaigns/join  # a friend joins
    curl -H "Authorization: Bearer $TOKEN" -d '{"pc":"fighter-1"}' localhost:8080/campaigns/$ID/claim
    curl -H "Authorization: Bearer $TOKEN" -X POST localhost:8080/campaigns/$ID/ready

A Player may claim both Fighters. The Campaign starts when everyone is ready and both Fighters are claimed. With `-dev`, `POST /campaigns/solo {"adventure":"arena"}` starts one with a single Player controlling both. Chat is a WebSocket at `/campaigns/$ID/chat`.

Then start one client per Fighter with Godot 4.7. Each client plays the Player Character named in its address, with its Player's token, and sees only what that character perceives:

    godot --path client -- --token=$TOKEN --server="ws://127.0.0.1:8080/play?campaign=$ID&pc=fighter-1"
    godot --path client -- --token=$TOKEN --server="ws://127.0.0.1:8080/play?campaign=$ID&pc=fighter-2"

The game waits on a Fighter's turn until its client acts. Click a blue square to move, a red square to attack, a green square to stabilize a dying ally, and use the buttons for the other actions. Q and E turn the camera, and the mouse wheel zooms.
