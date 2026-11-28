# Install conn from source

Conn has no prebuilt release yet. These steps build `conn` and its local TypeScript analyzer from the Conn source repository. They need Git, Go 1.27.4, Node.js 26.13.1, and pnpm 12.21.0, and take a few minutes the first time.

Show each command to the human and run it only after the human confirms. Stop at the first failure and report it. Never download and run a prebuilt executable instead.

## 1. Clone

Choose a folder outside the current workspace, then clone the repository into it:

```text
git clone https://github.com/Juanqu261/conn.git
cd conn
```

When the repository has release tags, check out the newest one with `git checkout <tag>`.

## 2. Build the analyzer

```text
pnpm install --frozen-lockfile
pnpm build
```

## 3. Build the executable

Build in the same folder, so that `conn` finds the analyzer beside it.

```text
go build -o conn ./cmd/conn
```

On Windows, name the output `conn.exe`. On a release tag, stamp the version with `-ldflags "-X main.version=<tag>"`; a build without it reports `dev`.

## 4. Put the folder on PATH

Add the folder to `PATH` for new terminals and for Kiro, then restart Kiro so that the power's server starts from the new executable.

- macOS and Linux: add the folder to `PATH` in the shell profile.
- Windows: add the folder to the user `Path` environment variable in System Properties.

## 5. Confirm

Run `conn --version` and `conn doctor`, then continue with the `conn-setup` skill.
