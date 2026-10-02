# Postmortem: <short title>

> Blameless: describe what the *system* allowed to happen, not who typed what.
> Copy this file to `YYYY-MM-DD-short-title.md`.

| | |
|---|---|
| **Date** | |
| **Duration** | detection → mitigation → resolution |
| **Severity** | page / ticket / drill |
| **Error budget consumed** | % of the 30-day budget (see docs/slo.md queries) |
| **Trigger** | drill # / real change / unknown |

## Summary
Two or three sentences: what broke, who or what was affected, how it was fixed.

## Impact
Numbers, not adjectives: failed requests (from `load.sh` or Prometheus), minutes of degraded service, deploys blocked.

## Timeline (all times local)
| Time | Event |
|---|---|
| hh:mm | change / fault introduced |
| hh:mm | first symptom (which signal: alert, pipeline, load.sh, manual check?) |
| hh:mm | diagnosis: key command and what it showed |
| hh:mm | mitigation |
| hh:mm | resolved |

## What I expected vs what happened
The drill hypothesis next to the observation. The gap is the learning.

## Diagnosis
How the cause was found: the commands, their output (trimmed), and the dead ends. Dead ends are worth keeping; they show how you reason.

## Root cause and contributing factors
- Root cause:
- Contributing: (why was detection slow? why was the blast radius that size?)

## What went well
-

## What went wrong / where we got lucky
-

## Action items
| Action | Type (prevent / detect / mitigate) | Status |
|---|---|---|
| | | |
