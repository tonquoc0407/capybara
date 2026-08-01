# Detector corpus

Labelled traces that pin what each detector fires on. Every trace is one run in
span-per-line jsonl; `labels.json` records the finding types each run should
carry. `capybara eval` re-analyses the imported runs and scores precision,
recall and F1 per type, with a run as the unit.

The corpus is adversarial by design: alongside a positive for each type it
carries near-miss negatives — a hedged answer after a failed tool, a document
that discusses prompt injection without carrying one, a loop of one tool over
distinct arguments, a widened schema that did not break. Those are what keep the
score honest; a detector that reaches for recall by tripping them loses
precision here before it does in the field.

The numbers are a regression gate, not a claim about live traffic: the inputs
are curated, so a perfect score means the detectors meet their spec on these
scenarios, and a drop means one changed behaviour.

## Run

```sh
sh corpus/run.sh
```

Non-zero exit when any exercised type scores below F1 1.0 (override with
`FAIL_UNDER`). CI runs this on every push.

## Extend

Add a `traces/<name>.jsonl` and a case in `labels.json`. Span ids are a global
key, so prefix them per run; tool names are keyed globally too, so give a run
its own so its schema learning stays isolated. `expect` lists the scored types
the run should carry; an empty list marks a run that must stay clean.
