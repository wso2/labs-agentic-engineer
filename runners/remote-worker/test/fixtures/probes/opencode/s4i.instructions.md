# Project skills

No skill is preloaded for this probe.

## Tool glossary (OpenCode)

The roles your workflow names, and the tools that play them in this session:

- **fan-out tool**: `task` with `subagent_type: "general"`. There is no background mode in this session: "dispatch in the background" means issue every `task` call of the wave as PARALLEL tool calls in ONE message. They run at the same time and each returns its builder's report when that builder finishes; you are held until the slowest one does.
- **wait tool**: none is needed. Each `task` call IS the wait: its result is the report.
- **stop tool**: none in this session; a task you no longer need is left to finish
- **task list**: `todowrite`
- **edit**: `edit`, `write` · **shell**: `bash`

Glossary marker: GLOSSARY-MARKER-9931
