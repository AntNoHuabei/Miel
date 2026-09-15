# Agent Runtime Conventions

- On Windows, every child process started by the application must hide its console window by calling the shared `hideProcessWindow` helper before `Start` or `Run`.
- This applies to shell commands, Skill runtimes, package managers, Explorer, and native file-open helpers.
- Keep the helper a no-op on non-Windows platforms; do not add Windows-only `SysProcAttr` fields to cross-platform source files.
- Use argument arrays and explicit working directories. Do not construct shell commands by concatenating untrusted input.
