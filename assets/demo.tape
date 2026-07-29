# Regenerate assets/demo.gif:
#
#   brew install vhs     # pulls in ttyd + ffmpeg
#   go install .         # so the recording runs the current build
#   vhs assets/demo.tape
#
# The tape seeds a throwaway board in a temp dir, so recording never touches a
# real board or config.

Require shepherd

Output assets/demo.gif

Set Shell zsh
Set FontSize 15
Set Width 1320
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"
Set Framerate 24
Set TypingSpeed 45ms

Hide
# interactive zsh would eat the !h/!l priority tokens as history expansion
Type 'setopt nobanghist'
Enter
Type 'd=$(mktemp -d) && export SHEPHERD_TODO_FILE=$d/demo.md SHEPHERD_CONFIG=$d/config.toml'
Enter
Type 'shepherd add "cut the 1.0 release tag @release !h due:tomorrow" >/dev/null'
Enter
Type 'shepherd add "verify the signed artefacts @release status:in-progress" >/dev/null'
Enter
Type 'shepherd add "document the config file @docs due:week" >/dev/null'
Enter
Type 'shepherd add "port the key table to a grid @docs !l" >/dev/null'
Enter
Type 'shepherd add "fix the flaky lock test @bugs !h" >/dev/null'
Enter
Type 'shepherd add "reply on the mailing list thread" >/dev/null'
Enter
Type 'shepherd sub 1 "tag the commit" >/dev/null'
Enter
Type 'shepherd sub 1 "push the tap bump" >/dev/null'
Enter
Type 'clear'
Enter
Show

Type "shepherd"
Enter
Sleep 2.5s

# walk the board
Down@450ms 3
Sleep 1.2s

# add: the new item inherits the selected row's category
Type "a"
Sleep 600ms
Type "write the release notes"
Sleep 800ms
Enter
Sleep 2s

# tick it off
Space
Sleep 1.8s

# detail view, then tag it from there
Type "d"
Sleep 2.2s
Type "T"
Sleep 600ms
Type "docs,launch"
Sleep 600ms
Enter
Sleep 1.8s
Escape
Sleep 1.2s

# cycle the views
Type "v"
Sleep 1.8s
Type "v"
Sleep 1.8s
Type "v"
Sleep 1.8s

# filter as you type
Type "/"
Sleep 400ms
Type "release"
Sleep 1.8s
Escape
Sleep 1.2s

Type "q"
Sleep 1.5s
