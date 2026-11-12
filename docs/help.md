# help

Read when: discovering command usage from the CLI itself.

`wacli help` is the Cobra-provided help command. Every command also accepts `--help`.
Running a command this build removed prints `wacli read-only build: this binary cannot send to or change WhatsApp: "wacli <command>" is not available` and exits 1.

## Commands

```bash
wacli help [command]
wacli [command] --help
```

## Examples

```bash
wacli help sync
wacli messages search --help
wacli groups participants list --help
```
