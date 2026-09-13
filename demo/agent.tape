# the agent loop in the scripted demo ui: prompt, build, a failing check, repair, pass
Output demo/assets/agent.gif

Require vhs
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 700
Set Padding 20
Set Framerate 24
Set TypingSpeed 45ms
Set WaitTimeout 90s
Set Theme { "name": "pantera", "background": "#201F26", "foreground": "#ECEBF0", "black": "#2D2C36", "red": "#FF577D", "green": "#12C78F", "yellow": "#F5EF34", "blue": "#6B50FF", "magenta": "#FF60FF", "cyan": "#00A4FF", "white": "#BFBCC8", "brightBlack": "#4D4C57", "brightRed": "#FF6DAA", "brightGreen": "#00FFB2", "brightYellow": "#E8FE96", "brightBlue": "#7272FF", "brightMagenta": "#FF84FF", "brightCyan": "#4FBEFE", "brightWhite": "#F7F6FB", "selection": "#3A3943", "cursor": "#FF60FF" }
Env PS1 "$ "
Env HH_NETWORK "mock"

Hide
Type "go build -o /tmp/hh-demo ./cmd/hh-tui-demo && clear" Enter
Sleep 8s
Type "/tmp/hh-demo" Enter
Sleep 2s
Show

Sleep 1500ms
Type "build a page where I can swap HBAR for USDC"
Sleep 800ms
Enter
Wait+Screen /done in|gave up/
Sleep 4s
Hide
Ctrl+C
Ctrl+C
