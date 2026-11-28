---
status: accepted
date: 2026-11-28
decision-makers: Ivan Lee
---

# Leave command-line parsing to cobra, and print nothing from variant on a bad command line

## Context and Problem Statement

Up to v0.2.0, `main.go` re-implemented pflag's parsing rules in two places. `optionBeforeCommand` found an option written before `check` or `variant`, so that `releash --loud check x` could exit 2 with the fallback answer instead of failing on the root command with empty stdout. `scanArgs` re-read the raw arguments when cobra could not parse them, to find the `--format` and `--prefix` that `variant`'s fallback should be printed in. Both tracked which options take a value, shorthand runs, `--`, and command names consumed as option values: about 120 lines of product code, a large share of `main_test.go`, and several paragraphs of spec 0003 and the internals doc, all for invocations that are already errors. Any change to the option set, or a pflag upgrade, could make the scanners disagree with the real parser without a test noticing ([#35](https://github.com/ivanklee86/releash/issues/35)).

How should releash handle options before the command, and what should `variant` print when its command line cannot be parsed?

## Decision Drivers

* Only one parser should decide what a command line means.
* `check`'s stdout stays `true` or `false` on every exit releash controls ([ADR 0004](0004-exit-code-contract.md)).
* Existing exit codes keep their meaning.
* A pipeline that gets the command line wrong should fail on its first run.

## Considered Options

* Shared options as persistent flags of the root command, so they may go before the command name.
* Keep rejecting options before the command, but find them with pflag instead of by hand.
* For a bad command line, read `variant`'s `--format` and `--prefix` by parsing a copy of its flag set with pflag, ignoring unknown options.
* `variant` prints nothing on exit 2.

The first two answer where options may go; the last two answer what `variant` prints on exit 2.

## Decision Outcome

Chosen options: "Shared options as persistent flags of the root command" and "`variant` prints nothing on exit 2", because together they remove every hand-written scan while `check` keeps printing `false` on every exit.

* The options that `check` and `variant` share are persistent flags of the root command. cobra parses `releash --loud check x` and `releash --format shell variant x` as it parses the same options after the command. They are hidden from the root command's help, which lists only `--help` and `--version`.
* cobra still blames the root command for some command lines meant for `check`: an unknown option before the command (`releash --nope check x`), and an option that takes the command name as its value (`releash --token $TOKEN check x` with an empty `$TOKEN`). For those, releash prints the fallback answer of the first argument that names `check` or `variant`. Matching a word does not depend on pflag's rules, so it cannot drift from them. When cobra reports an unknown command and the argument before the command name is an option, stderr adds a line naming that option as possibly missing its value.
* On exit 2, `variant` prints nothing on stdout, whether the problem is cobra's (an unknown option, a missing argument) or found by validation (an invalid or repeated `--format` or `--prefix`, an invalid `--timeout`). On every other exit that releash controls it prints the disabled variant as before.
* `check` is unchanged: it prints `false` on exit 2.

### Consequences

* Good, because `optionBeforeCommand` and `scanArgs` are gone, with their tests and spec text, and nothing can disagree with pflag.
* Good, because `releash --loud check x`, which used to exit 2, now works.
* Good, because `variant` no longer needs rules for guessing the format and prefix of an unparseable command line.
* Bad, because a `variant` step that ignores the exit code gets no variables on a bad command line, where it used to get the disabled variant. Under `set -e`, or with `$GITHUB_ENV`, the step fails either way, and `${VAR:-default}` still falls back. This changes the output contract, so it ships in a minor version (v0.3.0) under "Breaking changes", as spec 0002's versioning policy requires.
* Neutral, because a shared option given without a command, as in `releash --loud`, needs its own check to keep exiting 2: the root command now knows the option, so cobra would accept it and print help.

### Confirmation

`main_test.go` covers options before each command, the root-command errors above printing `false` for `check` and nothing for `variant`, exit 2 printing nothing for `variant` with and without `--format`, and root help hiding the shared options. `internal/releash` tests check that `Variant` prints nothing on exit 2 and the disabled variant on exits 3 to 5.

## Pros and Cons of the Options

### Persistent flags on the root command

* Good, because cobra parses the whole command line with the command's own flags, so there is nothing to scan.
* Bad, because the root command's help would list every shared option, so they are hidden there.
* Bad, because cobra blames the root command for an unknown option before the command, which needs the one-word lookup above.

### Keep rejecting options before the command

* Good, because there is one place for options and the README rule stays short.
* Bad, because cobra's subcommand lookup guesses whether an unknown option takes a value, so releash still has to find the command and the option itself before cobra runs.

### Parse a copy of `variant`'s flag set with pflag

* Good, because stdout keeps the disabled variant on exit 2.
* Bad, because pflag stops at the first parse error and gives an unknown option the next word as its value, so spec 0003 would still need rules for those cases, and the result would be printed for command lines that are wrong anyway.

### Print nothing on exit 2

* Good, because no scan of any kind is needed.
* Bad, because it changes the output contract (see Consequences).

## More Information

This amends [ADR 0004](0004-exit-code-contract.md), which said that `variant` prints the disabled variant on every non-zero exit, and [spec 0003](../specs/0003-variant-variables.md), "Output and exit codes". See [spec 0001](../specs/0001-rollout-decisions.md), "Exit codes", for `check`.
