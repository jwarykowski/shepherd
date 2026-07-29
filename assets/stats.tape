# Regenerate assets/stats.gif:
#
#   brew install vhs     # pulls in ttyd + ffmpeg
#   go install .         # so the recording runs the current build
#   vhs assets/stats.tape
#
# Same throwaway board as demo.tape (assets/demo-seed.sh), whose archive carries
# a few weeks of completed work so the sparkline and backlog trend are populated.
# Smaller font than the board recording: `shepherd stats` is 56 rows tall.

Require shepherd

Output assets/stats.gif

Set Shell zsh
Set FontSize 12
Set Width 1400
Set Height 1000
Set Padding 20
Set Theme "Catppuccin Mocha"
Set Framerate 24
Set TypingSpeed 60ms

Hide
Type 'd=$(mktemp -d) && export SHEPHERD_TODO_FILE=$d/todo.md SHEPHERD_CONFIG=$d/config.toml'
Enter
Type 'sh assets/demo-seed.sh'
Enter
Type 'clear'
Enter
Show

Type "shepherd stats"
Sleep 800ms
Enter
Sleep 6s
