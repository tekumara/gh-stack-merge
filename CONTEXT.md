# Sequential stack merging

This extension merges the pull requests in a GitHub stack in order while showing progress.

## Language

**Stack**:
An ordered chain of pull requests rooted on a trunk branch. Lower pull requests must merge before those above them.
_Avoid_: PR group

**Layer**:
One branch and its pull request within a stack.
_Avoid_: Step

**Merge step**:
The period from selecting the next unmerged pull request until GitHub reports it as merged.
_Avoid_: Layer

**Sequential stack merge**:
A run that completes one merge step at a time from the bottom of a stack to the top.
_Avoid_: Atomic stack merge, batch merge
