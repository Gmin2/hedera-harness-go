# what it takes to build the same swap dapp: official hedera-harness vs hh
# run from the repo root: vhs demo/compare.tape
Output demo/out/compare.mp4

Require vhs
Set Shell "bash"
Set FontSize 22
Set Width 1600
Set Height 900
Set Padding 30
Set WindowBar Colorful
Set TypingSpeed 35ms
Set Theme { "name": "pantera", "background": "#201F26", "foreground": "#ECEBF0", "black": "#2D2C36", "red": "#FF577D", "green": "#12C78F", "yellow": "#F5EF34", "blue": "#6B50FF", "magenta": "#FF60FF", "cyan": "#00A4FF", "white": "#BFBCC8", "brightBlack": "#4D4C57", "brightRed": "#FF6DAA", "brightGreen": "#00FFB2", "brightYellow": "#E8FE96", "brightBlue": "#7272FF", "brightMagenta": "#FF84FF", "brightCyan": "#4FBEFE", "brightWhite": "#F7F6FB", "selection": "#3A3943", "cursor": "#FF60FF" }

Env PS1 "$ "

Hide
Type "cd demo/official && clear" Enter
Show

Type "# official hedera-harness: build a feature by writing a recipe first" Enter
Sleep 800ms
Type "find .harness -type f | xargs wc -l" Enter
Sleep 2500ms
Type "head -12 .harness/spec.yaml" Enter
Sleep 3s
Type "# then: hedera-harness run   (unattended, 40 min to 2 h, testnet only, llm reads curl to judge the chain)" Enter
Sleep 3s
Type "clear" Enter

Type "# hh: describe the dapp in one line" Enter
Sleep 800ms
Type "cat ../../../swap-dapp/hh.yaml | grep -vE '^\s*#|^\s*$'" Enter
Sleep 3s
Type "# hh  >  build a page where I can swap HBAR for USDC at a fixed rate" Enter
Sleep 3s
