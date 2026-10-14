---
name: file-github-issue
description: File a GitHub issue for this project, checking for duplicates, selecting repository labels, linking code with permalinks, and recording issue relationships through the API. Use when the requestor asks to file or open an issue.
---

# File a GitHub issue

Use the repository selected by the requestor; otherwise derive it from this
project's GitHub remote. Prefer available GitHub tools or `gh` for authenticated
operations. Filing an issue is authorized by a request to file one; a request
for a draft authorizes only preparing the draft.

## Check for existing issues first

Before creating anything, search both open and closed issues in the target
repository. Search several relevant terms, including symptoms, affected
components, error messages, and alternate wording. Inspect plausible matches'
bodies and relevant comments rather than relying on their titles. Retrieve
additional pages when needed; a failed or truncated search is not evidence
that no matching issue exists.

If an issue is the same or similar, show the requestor its link and explain the
overlap. Ask what they want to do: use the existing issue, add information to
it, or create a distinct issue. Wait for their answer before filing or changing
an existing issue. Do not infer permission from silence. This applies to closed
matches too. A known dependency covering different work is a relationship,
not necessarily a duplicate.

## Prepare the issue

Read the repository's issue templates and follow the applicable format. Describe
the concrete problem or requested behavior and include supporting evidence.
Keep the title focused on the work. Do not put labels or classification prefixes
such as `[bug]`, `enhancement:`, or priority tags in the title.

Fetch the repository's current labels, including all pages, and inspect their
names and descriptions. Assign existing labels appropriate to the issue's
context using their exact names. Do not invent or create labels unless requested.
Leave labels empty if none fit.

For every reference to existing code on `main`, resolve the current remote
`main` commit SHA and inspect the file at that commit. Use a Markdown permalink
in this form:

```text
https://github.com/OWNER/REPO/blob/FULL_COMMIT_SHA/path/to/file#L10-L20
```

Use the relevant line or range for a symbol or excerpt; a whole-file reference
may omit the fragment. Link named files and symbols instead of leaving bare
paths or names as the only reference. Verify paths and line numbers against
that same SHA. Do not use `/blob/main/`, stale local line numbers, or a feature
branch commit when describing existing code on `main`.

## Record relationships through the API

Use GitHub's Relationships APIs for known blockers, issues this work blocks,
related issues, and parent/sub-issue relationships where applicable. Do not
replace these with body sections, task lists, or comments saying “blocked by,”
“blocks,” or “related to.” Record only relationships supported by the request
or evidence; do not turn a thematic connection into a dependency.

After creating the issue, use the returned issue number to add relationships:

| Relationship | API operation |
| --- | --- |
| New issue is blocked by B | `POST /repos/{owner}/{repo}/issues/{new_number}/dependencies/blocked_by` with B's numeric `issue_id` |
| New issue blocks B | `POST /repos/{B_owner}/{B_repo}/issues/{B_number}/dependencies/blocked_by` with the new issue's numeric `issue_id` |
| New issue relates to B | `POST /repos/{owner}/{repo}/issues/{new_number}/relates_to` with B's numeric `issue_id` |
| New issue is a child of P | `POST /repos/{P_owner}/{P_repo}/issues/{P_number}/sub_issues` with the new issue's numeric `issue_id` |

Fetch each target issue to obtain its REST `id`; an issue number or GraphQL
`node_id` is not the numeric `issue_id`. With `gh api`, use `-F issue_id=ID`
to send a numeric value. Check existing relationships before adding them.
Consult the current official documentation for API version, permissions, and
constraints when needed:

- [Issue dependencies](https://docs.github.com/en/rest/issues/issue-dependencies)
- [Related issues](https://docs.github.com/en/rest/issues/issues#add-a-related-issue)
- [Sub-issues](https://docs.github.com/en/rest/issues/sub-issues)

## Create and verify

Create the issue with the prepared title, body, and selected labels. Use a
structured body argument or, with `gh issue create`, `--body-file` to preserve
Markdown without shell interpolation. Add the known relationships, then read
back the issue and relationship endpoints to verify the saved result.

If creation has an ambiguous outcome, check whether it succeeded before
retrying. If a label or relationship operation fails, report the issue URL and
the specific incomplete operation; do not create another issue or silently
substitute relationship text. Finish by returning the issue link, assigned
labels, and recorded relationships, including any remaining failures.
