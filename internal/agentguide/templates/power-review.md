## Evidence labels and the pull request

- Evidence from `conn evidence run` is labelled observed on this machine. Evidence attached through `attach_evidence` or `conn evidence attach` without an observation is labelled reported. Keep each label as the review packet states it, and never describe local or reported evidence as CI proof.
- The review packet from `read_review_packet` lists each acceptance criterion with its evidence state, scope drift, changed files without a declared claim, and collisions with their decisions.
- To open a pull request with the packet as its description, save the Markdown that `{{prompt:conn-review}}` produces to a file and suggest `gh pr create --body-file <file>`. The human runs it or approves it in the shell.
- When `.conn/checks.json` is missing, suggest that the human define checks there. Do not invent check commands or run them unasked.
