### `unity` - run experiments/regression tests on CUE modules

`unity` is a tool used to run experiments/regression tests on various CUE modules using different versions of CUE.  The
repository that contains the `unity` tool (this repository) is also a corpus of CUE modules against which `unity` is
run. `unity` is based in part on the ideas behind [Rust's](https://www.rust-lang.org/)
[`crater`](https://github.com/rust-lang/crater).

`unity test` is currently the only implemented command.

The main features of `unity test` are:

* simple specification of tests and expectations via
  [`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) test scripts
* running of Go tests for those packages in a project that use the CUE Go API (coming soon)
* cross platform (currently only Linux tested) support for running `unity`
* can be run locally or triggered to run as a GitHub Actions workflow via the
  [`cueckoo`](https://github.com/cue-sh/tools/tree/master/cmd/cueckoo) command
* by default runs in "safe" mode where tests are run in a Docker container on a volume-mounted copy of the CUE module
  under test
* multiple ways of specifying CUE versions to test against (see below)
* a `--corpus` mode for running `unity` across the `git` submodules of a project. The `unity` project itself uses this
  mode to test the corpus under https://github.com/cue-sh/unity/tree/main/projects
* an `--update` flag to update golden files in a project when a `cmp`-esque comparison in a `testscript` test script
  fails
* easy addition of new projects to the `unity` corpus

### How do I add my project to the `unity` corpus?

Simple!

* Add a new `git` submodule;
* Create a PR;
* Request a review;
* Wait for the CI tests for pass;
* We will merge, everyone will benefit!

For projects which define their own `cue.mod/tests` manifest, you can use the
[PR where `unity-example` was added to the
corpus](https://github.com/cue-sh/unity/pull/23) as an example.

Where a project cannot (yet) define such a manifest, the [`vector` project
PR](https://github.com/cue-sh/unity/pull/33) provides an example of how to also
define an overlay.

### Motivation

CUE is currently missing:

* `cue test` (see https://github.com/cuelang/cue/issues/209)
* full dependency management via modules (see https://github.com/cuelang/cue/issues/434)
* a module discovery site/API similar to [pkg.go.dev](https://pkg.go.dev)

Hence:

* we don't have a good way of discovering CUE modules in the wild
* module authors don't have a way to assert correctness of their CUE configurations with respect to a given version of
  CUE, either via `cmd/cue` commands or via the `cuelang.org/go/...` package APIs

`unity` is intended as a stepping stone towards these missing features. It will also:

* allow the CUE project to move more quickly with changes/performance improvements, because we have a corpus of code
  against which to regression test/experiment
* allow analysis of how CUE is being used in the wild, to see whether aspects of CUE can be improved (e.g. better `cue
  vet` rules, or different package/input modes)

### Using `unity`

`unity test` is currently the only implemented command. `unity test` works in two modes: project mode (default) or
`--corpus` mode.

#### Project mode

In project mode, `unity` runs within the context of a project that declares the `unity` manifest within the
`cue.mod/tests` directory. The `unity` manifest is CUE package value that satisfies the
[`#Manifest`](https://github.com/cue-sh/unity/blob/de07b0f83e70913697b2f70f660db888d11059d4/unity_go_gen.cue#L9-L14)
definition. The [`unity-example`](https://github.com/cue-sh/unity-example) project [declares such a
manifest](https://github.com/cue-sh/unity-example/blob/50254fe95093f9460a0e12debf7b4684763a1a5c/cue.mod/tests/tests.cue):

```
package tests

Versions: ["go.mod", "v0.3.0-beta.5"]
```

Via such a manifest a project declares the latest versions of CUE against which its configurations are known to be
correct, or more precisely against which its `unity` tests are known to pass.

The `cue.mod/tests` directory also contains a number of
[`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) test scripts. Again, considering the
`unity-example` project, it defines a basic
[`eval.txt`](https://github.com/cue-sh/unity-example/blob/50254fe95093f9460a0e12debf7b4684763a1a5c/cue.mod/tests/eval.txt)
test script as follows:

```
# Verify that eval works as expect

cue eval ./...
cmp stdout $WORK/stdout.golden

-- stdout.golden --

x: 5
```

Every such test script is run:

* within a Docker container, unless `--unsafe` is provided
* within a clean working directory, referred to as `$WORK` (see the `testscript` documentation for more details)
* with a minimal environment (see the `testscript` documentation for more details)
* with a copy of the repository containing the CUE module under test available at `$WORK/repo`
* with all files in the test script archive expand to `$WORK`
* with an initial working directory of `$WORK/repo/path/to/module` for convenience

Hence the above example test script makes a copy of `unity-example` available at `$WORK/repo`. The script has an initial
working directory of `$WORK/repo`, because the `unity-example` CUE module is defined at the root of that project
repository. Therefore, the script runs `cue eval ./...` in the context of a copy of the CUE module under test. The
golden file `stdout.golden` is extracted to `$WORK`, hence the comparison `cmp stdout $WORK/stdout.golden` needs to
specify the full path to `stdout.golden` because the working directory is `$WORK/repo`.

Here is the output from running `unity` within the `unity-example` project:

```
$ go run github.com/cue-sh/unity/cmd/unity test --verbose commit:91abe0de26571ef337559580442f990ded0b32f9
--- PASS: eval/go.mod

        WORK=$WORK
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        HOME=/no-home
        TMPDIR=$WORK/tmp
        devnull=/dev/null
        /=/
        :=:
        exe=

        # Verify that eval works as expect (0.043s)
        > cue eval ./...
        [stdout]
        x: 5

        > cmp stdout $WORK/stdout.golden
        PASS
--- PASS: eval/v0.3.0-beta.5

        WORK=$WORK
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        HOME=/no-home
        TMPDIR=$WORK/tmp
        devnull=/dev/null
        /=/
        :=:
        exe=

        # Verify that eval works as expect (0.042s)
        > cue eval ./...
        [stdout]
        x: 5

        > cmp stdout $WORK/stdout.golden
        PASS
--- PASS: eval/commit:91abe0de26571ef337559580442f990ded0b32f9

        WORK=$WORK
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        HOME=/no-home
        TMPDIR=$WORK/tmp
        devnull=/dev/null
        /=/
        :=:
        exe=

        # Verify that eval works as expect (0.043s)
        > cue eval ./...
        [stdout]
        x: 5

        > cmp stdout $WORK/stdout.golden
        PASS
```

First, the two base versions declared as supported in the `unity-example` manifest are run. Then, the command line
version `commit:91abe0de26571ef337559580442f990ded0b32f9` is also tested, which is a reference to a [commit referenced
by the `master` branch](https://github.com/cuelang/cue/tree/91abe0de26571ef337559580442f990ded0b32f9) of the CUE
project.

Any project that uses CUE can also use `unity` as part of its own testing/CI regime. For example, the [Play with
Go](https://play-with-go.dev/) project [runs `unity` as part of its GitHub
workflows](https://github.com/play-with-go/play-with-go/blob/2c980fc5b1956bb05f259b986dd18d9f58efe869/.github/workflows/test.yml#L65-L66).
This fits nicely with the fact that the Play with Go project is [also part of the `unity`
corpus](https://github.com/cue-sh/unity/tree/de07b0f83e70913697b2f70f660db888d11059d4/projects/github.com/play-with-go).

#### `--corpus` mode

In `--corpus` mode, `unity` tests all of the `git` submodules of a repository in project mode. Taking the `unity`
repository itself as an example corpus:

```
$ go run github.com/cue-sh/unity/cmd/unity test --corpus --overlay overlays --nopath refs/changes/41/8841/3
testing projects/github.com/play-with-go/play-with-go against version go.mod
testing projects/github.com/cue-sh/unity-example against version go.mod
testing projects/github.com/cue-sh/unity-example against version v0.3.0-beta.5
testing projects/github.com/timberio/vector against version v0.3.0-beta.5
testing projects/github.com/TangoGroup/cfn-cue against version v0.3.0-beta.5
testing projects/github.com/play-with-go/play-with-go against version refs/changes/41/8841/3
testing projects/github.com/cue-sh/unity-example against version refs/changes/41/8841/3
testing projects/github.com/timberio/vector against version refs/changes/41/8841/3
testing projects/github.com/TangoGroup/cfn-cue against version refs/changes/41/8841/3
```

In this case the base versions declared as supported by each project in the corpus are tested first. Then the command
line specified `refs/changes/41/8841/3` is also tested. This is a reference to a [CL that was in progress at the
time](https://cue-review.googlesource.com/c/cue/+/8841/2) (since merged).

### Specifying CUE versions

`unity` supports different ways of specifying the CUE version against which to test:

* `go.mod` - the version of CUE resolved via the Go module in which the CUE module under test is found
* `$semver` - any official [CUE (pre)release](https://github.com/cuelang/cue/releases), e.g. `v0.3.0-beta.5`
* `/path/to/cue` - an absolute path to the location of a Go module where `cuelang.org/go` can be resolved (this could be
  the CUE project itself)
* `PATH` - use the `cue` command found on your `PATH`. This binary must be compiled for the operating system and
  architecture of the target Docker image if you are running in normal/safe mode
* `commit:$hash` - a commit on the `master` branch of the [CUE project](https://github.com/cuelang/cue), e.g.
  [`commit:a0e19707b99d8e76caf3234c42761a73d0fb85f7`](https://github.com/cuelang/cue/commit/a0e19707b99d8e76caf3234c42761a73d0fb85f7)
* `$CLref` - a [CUE project Gerrit](https://cue-review.googlesource.com) CL patchset reference, e.g.
  `refs/changes/21/8821/3`

### FAQ

Please see [the wiki FAQ](https://github.com/cue-sh/unity/wiki/FAQ).

