# Write rules with your own AI

RuleSend helps you review AI-generated edits without connecting to an AI provider.
Your AI proposes changes; you decide what to save and publish.

1. Open **Rule packs**, select a pack, and choose **Ask my AI** in **Write & preview**.
2. Describe the behavior you want. Choose **Generate prompt**, review the complete
   prompt, and select **Copy prompt**. Paste it into an AI allowed to receive that
   content. The prompt contains the selected local draft, including unsaved edits,
   and your request. It excludes other packs, device inventory, and Hub credentials.
3. Copy the AI's complete JSON response. Back in the same pack, choose
   **Import proposal** and **Review changes**. A single JSON code block is accepted.
4. Read the highlighted added/removed lines, both full documents, the AI's summary,
   reported risks, and suggested checks. These explanations are unverified; the
   suggested checks have not run. Review removed protections even if the AI reports
   no risks. Select the review checkbox and **Approve to draft** when satisfied.
5. Choose **Save draft**, then **Publish version** and confirm the delivery scope.
   For source-backed packs, Git recording must complete before Hub publication.
   Check device receipts afterward. Installation does not prove AI loading or behavior.

Approval changes only the local, unsaved draft. Cancel leaves it unchanged. No
prompt, request, or imported proposal is persisted in browser storage. Closing the
tab loses unsaved work; save an approved draft to retain it in the Hub. The copied
prompt remains in the system clipboard and any AI conversation you paste it into.

An imported proposal must match the selected pack, exact draft content hash, and
draft revision. Approval rechecks the current saved Hub draft as well as local
edits. If the draft changed, resolve the conflict and generate a fresh prompt.
You can import in another browser if its current draft and revision match. A
response based on unsaved text requires those same local draft bytes.

## Writing useful instructions

Keep instructions short, concrete, and testable. Specify when a rule applies and
any exceptions. Keep personal preferences separate from project/company policy
and device paths. Remove redundant or conflicting requirements instead of adding
more broad “always” instructions. Do not include secrets. Markdown instructions
guide AI behavior; application permissions and other enforcement remain necessary.

## Response format

The generated prompt supplies the pack ID, exact SHA-256, and revision. The AI must
preserve these fields and return the full Markdown document in `content`, not a
patch or an explanation. `summary`, `risks`, and `checks` are arrays of short text.
The content limit is 128 KiB; individual application targets can be stricter.
RuleSend managed-block markers must not be included. An empty proposal is allowed
only through the same explicit review, with a visible full-deletion warning.

```json
{
  "rulesendProposal": 1,
  "packId": "pack-from-the-generated-prompt",
  "baseRevision": 1,
  "baseHash": "exact-64-character-sha256-from-the-prompt",
  "content": "# Working preferences\n\nKeep replies concise.\n",
  "summary": ["Clarified the reply preference."],
  "risks": [],
  "checks": ["Ask a simple question and check the reply length."]
}
```

This format checks the proposal's baseline, not who wrote it or whether it is safe.
AI text is rendered as text; RuleSend does not execute it. Large rewrites use a
bounded diff and show at most 2,000 highlighted lines, with both complete documents
available below. Direct provider integration and automatic publication are outside
this workflow.
