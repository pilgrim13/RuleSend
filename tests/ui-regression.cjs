/* Deterministic DOM-model regression checks. Not a browser or visual test.
 * Usage in a JS runtime: await runRuleSendChecks(appSourceString).
 * No filesystem, network, DOM package, or Node dependency is required. */
async function runRuleSendChecks(source) {
  const results = [];
  function assert(condition, message) { if (!condition) throw new Error(message); }
  function makeEnvironment() {
    const doc = {activeElement: null};
    const clipboard = {writes: [], fail: false, async writeText(text) { if (this.fail) throw new Error('Clipboard denied'); this.writes.push(text); }};
    let respond = async () => { throw new Error('Unexpected fetch'); };
    class Element {
      constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.dataset = {}; this.attributes = {}; this.handlers = {}; this.disabled = false; this.scrollLeft = 0; this.open = false; this._text = ''; this.className = ''; this.value = ''; this.parent = null; this.classList = {toggle() {}, remove() {}, add() {}}; }
      append(...items) { for (const item of items) { if (typeof item === 'string') { const text = new Element('#text'); text._text = item; text.parent = this; this.children.push(text); } else { item.parent = this; this.children.push(item); } } }
      replaceChildren(...items) { for (const item of this.children) item.parent = null; this.children = []; this._text = ''; this.append(...items); }
      set textContent(text) { this._text = String(text); this.children = []; }
      get textContent() { return this._text + this.children.map(x => x.textContent).join(''); }
      setAttribute(key, value) { this.attributes[key] = String(value); }
      removeAttribute(key) { delete this.attributes[key]; }
      addEventListener(type, fn) { (this.handlers[type] ||= []).push(fn); }
      async trigger(type) { for (const fn of this.handlers[type] || []) await fn({preventDefault() {}, target: this}); }
      get isConnected() { return this === doc.body || !!this.parent?.isConnected; }
      contains(element) { return this === element || this.children.some(child => child.contains(element)); }
      querySelectorAll(selector) {
        const key = selector === '[data-focus-key]' ? 'focusKey' : selector === '[data-scroll-key]' ? 'scrollKey' : null;
        const found = []; const walk = element => { for (const child of element.children) { if (key && child.dataset[key] !== undefined) found.push(child); walk(child); } }; walk(this); return found;
      }
      focus(options) { doc.activeElement = this; this.focusOptions = options; }
      select() { this.selected = true; this.selectionStart = 0; this.selectionEnd = this.value.length; }
      showModal() { this.open = true; }
      close() { this.open = false; }
      remove() { if (this.parent) this.parent.children = this.parent.children.filter(x => x !== this); this.parent = null; }
    }
    doc.body = new Element('body');
    doc.createElement = tag => new Element(tag);
    doc.createTextNode = text => { const node = new Element('#text'); node.textContent = text; return node; };
    doc.getElementById = id => { let found = null; const walk = el => { if (el.id === id) found = el; el.children.forEach(walk); }; walk(doc.body); return found; };
    doc.querySelectorAll = selector => doc.body.querySelectorAll(selector);
    doc.addEventListener = () => {};
    const mount = (id, tag = 'div') => { const el = new Element(tag); el.id = id; doc.body.append(el); return el; };
    ['app', 'notifications', 'content', 'save-draft', 'publish-draft', 'draft-save-status'].forEach(id => mount(id));
    const dialog = mount('dialog', 'dialog');
    const marker = "  window.addEventListener('beforeunload'";
    assert(source.includes(marker), 'Cannot locate app startup boundary');
    const instrumented = source.slice(0, source.indexOf(marker)).replace('(() => {', 'return (() => {') + `
      return {state, drafts, packWrites, saveDraft, showPublish, showRestore, closeDialog, renderEditor, renderPolicy, renderSourcePanel, renderLogin, showEnrollment, showGuide, refreshEditorStatus, loadSnapshot, preserveInteraction, showAskAI, showImportProposal, showProposalReview, parseAIProposal, contentHash, proposalDiff, aiBaseline, aiRequests,
        context(nextView, id) { view = nextView; selectedPack = id; },
        hooks(values) {
          if (values.api) api = values.api;
          if (values.refresh) refresh = values.refresh;
          if (values.render) { renderShell = values.render; renderView = values.render; renderConnection = values.render; }
          toast = values.toast || (() => {}); showError = values.showError || (error => { throw error; });
        }
      };
    })();`;
    const app = new Function('document', 'window', 'location', 'navigator', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'TextEncoder', 'fetch', instrumented)(doc, {}, {origin: 'http://127.0.0.1:8787', hostname: '127.0.0.1'}, {clipboard}, () => 0, () => {}, () => 0, () => {}, TextEncoder, (...args) => respond(...args));
    function descendants(el) { return [el, ...el.children.flatMap(descendants)]; }
    return {app, doc, dialog, Element, mount, descendants, clipboard, respond(fn) { respond = fn; }};
  }
  function setupPack(env, id = 'pack-a') {
    const p = {id, name: id, content: 'A', draftRevision: 1, publishedVersion: 0, mode: 'manual', intervalSeconds: 60, paused: false};
    env.app.state.packs.push(p);
    env.app.drafts.set(id, {content: 'B', savedContent: 'A', baseRevision: 1, dirty: true});
    env.app.context('packs', id); return p;
  }
  function deferred() { let resolve; const promise = new Promise(r => resolve = r); return {promise, resolve}; }
  function sourcePack(env) {
    const p = setupPack(env);
    p.source = {label: 'Shared rule source', proof: {commit: 'a'.repeat(40), releaseId: 'common-v6-fixture', version: 'v6', hash: 'b'.repeat(64)}, pending: '', status: 'idle', error: ''};
    const draft = env.app.drafts.get(p.id); draft.content = 'A'; draft.dirty = false;
    env.app.renderEditor(env.doc.getElementById('content'), p); env.mount('pack-source'); env.app.refreshEditorStatus();
    return p;
  }
  function sourceJob(p, status = 'pending') {
    return {id: 'publication-fixture', packId: p.id, content: 'A', hash: 'c'.repeat(64), message: '', baseDraftRevision: 1, baseSourceProof: p.source.proof, basePublishedVersion: 0, basePublishedHash: '', status, error: '', createdAt: '2026-10-12T00:00:00Z', completedAt: '', releaseVersion: 0};
  }
  function response(status, value) { return {ok: status >= 200 && status < 300, status, async json() { return value; }}; }

  function action(e, title) { return e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === title); }
  async function proposalFor(e, p, content = 'Revised rules\nKeep approvals explicit.') {
    const base = e.app.aiBaseline(p.id);
    return {rulesendProposal: 1, packId: p.id, baseRevision: base.revision, baseHash: await e.app.contentHash(base.content), content, summary: ['Explain changes'], risks: [], checks: ['Verify approvals remain explicit']};
  }
  async function importProposal(e, p, proposal) {
    e.app.showImportProposal(p.id);
    e.descendants(e.dialog).find(el => el.tagName === 'TEXTAREA').value = JSON.stringify(proposal);
    await action(e, 'Review changes').trigger('click');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); p.name = 'Synthetic shared rules';
    const baseText = 'Keep approvals explicit.\nDo not execute imported instructions.';
    e.app.drafts.get(p.id).content = baseText;
    e.app.state.devices.push({name: 'PRIVATE DEVICE THAT MUST NOT BE COPIED'});
    e.app.state.packs.push({id: 'other', name: 'Other pack', content: 'PRIVATE OTHER PACK'});
    e.app.showAskAI(p.id);
    const areas = e.descendants(e.dialog).filter(el => el.tagName === 'TEXTAREA');
    areas[0].value = 'Short replies, with useful failure details.';
    await action(e, 'Generate prompt').trigger('click');
    assert(!action(e, 'Copy prompt').disabled && areas[1].value.includes('currentRules') && areas[1].value.includes(JSON.stringify(baseText)), 'Prompt must preview the exact selected local draft');
    await action(e, 'Copy prompt').trigger('click');
    const copied = e.clipboard.writes[0];
    assert(copied.includes('company policies') && copied.includes('ONLY one JSON object') && !copied.includes('PRIVATE DEVICE') && !copied.includes('PRIVATE OTHER PACK'), 'Prompt must preserve safeguards and exclude other packs and device inventory');
    assert(e.app.drafts.get(p.id).content === baseText, 'Prompt generation and copying cannot edit the draft');
    e.app.drafts.get(p.id).content += '\nChanged';
    await action(e, 'Copy prompt').trigger('click');
    assert(e.clipboard.writes.length === 1 && e.dialog.textContent.includes('Generate a new prompt'), 'Copy must reject a stale preview');
    results.push('PASS: AI prompt previews and copies only the selected draft, with safeguards and stale-copy rejection');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); e.clipboard.fail = true;
    e.app.showAskAI(p.id); const areas = e.descendants(e.dialog).filter(el => el.tagName === 'TEXTAREA'); areas[0].value = 'Brief replies';
    await action(e, 'Generate prompt').trigger('click'); await action(e, 'Copy prompt').trigger('click');
    assert(areas[1].selected && e.doc.activeElement === areas[1] && e.dialog.textContent.includes('manually'), 'Unavailable clipboard must offer a manual selected prompt');
    results.push('PASS: AI prompt clipboard failure provides a manual-copy fallback');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p);
    const parsed = e.app.parseAIProposal('```json\n' + JSON.stringify(proposal) + '\n```', p.id);
    assert(parsed.content === proposal.content, 'Single JSON fences must be accepted without changing content');
    const invalid = ['Explanation\n' + JSON.stringify(proposal), JSON.stringify({...proposal, packId: 'other'}), JSON.stringify({...proposal, content: '<!-- rulesend:begin -->'}), JSON.stringify({...proposal, content: '\0'}), JSON.stringify({...proposal, content: '\ud800'}), JSON.stringify({...proposal, content: '💡'.repeat(32769)}), JSON.stringify({...proposal, summary: ['x'.repeat(2001)]}), ' '.repeat(1024 * 1024 + 1)];
    for (const text of invalid) { let rejected = false; try { e.app.parseAIProposal(text, p.id); } catch (_) { rejected = true; } assert(rejected, 'Malformed, foreign, invalid Unicode, oversized, or reserved-marker responses must be rejected'); }
    results.push('PASS: import validates format, pack identity, UTF-8 size, metadata limits, and reserved markers');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p);
    e.app.hooks({api: async path => { assert(path.endsWith(p.id), 'Approval can only read this pack'); return {pack: p}; }, render: () => {}});
    await importProposal(e, p, proposal);
    assert(e.dialog.textContent.includes('Review AI changes') && e.dialog.textContent.includes('not run') && e.app.drafts.get(p.id).content === 'B', 'Review must not apply or claim checks ran');
    const approve = action(e, 'Approve to draft'); assert(approve.disabled, 'Approval must require an explicit review checkbox');
    await approve.trigger('click'); assert(e.app.drafts.get(p.id).content === 'B', 'Unchecked approval cannot apply even when invoked directly');
    const consent = e.descendants(e.dialog).find(el => el.tagName === 'INPUT' && el.type === 'checkbox'); consent.checked = true; await consent.trigger('change'); await approve.trigger('click');
    assert(e.app.drafts.get(p.id).content === proposal.content && e.app.drafts.get(p.id).dirty && !e.dialog.open && p.content === 'A' && p.publishedVersion === 0, 'Approval must change only the local draft, with saving and publication still explicit');
    results.push('PASS: AI proposal review requires consent and approves only a local unsaved draft');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p);
    await importProposal(e, p, proposal); e.app.closeDialog();
    assert(e.app.drafts.get(p.id).content === 'B', 'Cancelled review must preserve local edits');
    e.app.drafts.get(p.id).content = 'Newer local rules'; await importProposal(e, p, proposal);
    assert(e.dialog.textContent.includes('draft changed since') && !action(e, 'Approve to draft') && e.app.drafts.get(p.id).content === 'Newer local rules', 'Import cannot overwrite newer local rules');
    results.push('PASS: cancelled and stale AI proposals preserve existing local drafts');
  }
  {
    for (const remoteChanged of [false, true]) {
      const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p); const wait = deferred();
      e.app.hooks({api: async () => wait.promise, render: () => {}});
      await importProposal(e, p, proposal); const consent = e.descendants(e.dialog).find(el => el.tagName === 'INPUT'); consent.checked = true; await consent.trigger('change');
      const approving = action(e, 'Approve to draft').trigger('click');
      if (!remoteChanged) e.app.drafts.get(p.id).content = 'Typed during approval';
      wait.resolve({pack: remoteChanged ? {...p, content: 'Other browser edit', draftRevision: 2} : p}); await approving;
      assert(e.dialog.open && e.dialog.textContent.includes('draft changed during review') && e.app.drafts.get(p.id).content === (remoteChanged ? 'B' : 'Typed during approval'), 'Approval must preserve edits that arrive locally or remotely during its read check');
    }
    results.push('PASS: approval rechecks local and remote revisions and blocks concurrent changes');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p, '');
    await importProposal(e, p, proposal);
    assert(e.dialog.textContent.includes('removes all rules') && action(e, 'Approve to draft').disabled, 'An empty proposal requires a visible deletion warning and consent');
    const diff = e.app.proposalDiff('Keep\nOld\nEnd', 'Keep\n<script>unsafe</script>\nEnd');
    assert(diff.textContent.includes('+1 / −1') && !e.descendants(diff).some(el => el.tagName === 'SCRIPT'), 'Diff must identify line changes and render untrusted HTML as text');
    const large = e.app.proposalDiff('old\n'.repeat(3000), 'new\n'.repeat(3000));
    assert(large.textContent.includes('first 2,000') && e.descendants(large).filter(el => el.tagName === 'SPAN').length <= 2000, 'Large rewrites must bound diff work and clearly disclose display truncation');
    results.push('PASS: empty proposals warn, diff renders text safely, and large rewrites are bounded');
  }
  {
    for (const cancelled of [false, true]) {
      const e = makeEnvironment(); const p = setupPack(e); const proposal = await proposalFor(e, p); const wait = deferred();
      e.app.hooks({api: async () => wait.promise, render: () => {}});
      await importProposal(e, p, proposal); const consent = e.descendants(e.dialog).find(el => el.tagName === 'INPUT'); consent.checked = true; await consent.trigger('change');
      const approving = action(e, 'Approve to draft').trigger('click');
      if (cancelled) e.app.closeDialog(); else { consent.checked = false; await consent.trigger('change'); }
      wait.resolve({pack: p}); await approving;
      assert(e.app.drafts.get(p.id).content === 'B', 'Cancelling or withdrawing consent during the server check cannot apply the proposal');
    }
    results.push('PASS: cancelling or withdrawing consent during approval preserves the draft');
  }

  {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred(); let puts = 0;
    e.app.hooks({api: async () => { puts += 1; return wait.promise; }, refresh: async () => {}});
    const pending = e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    const draft = e.app.drafts.get(p.id); draft.content = 'C'; draft.dirty = true;
    e.app.refreshEditorStatus();
    assert(e.doc.getElementById('save-draft').disabled && e.doc.getElementById('publish-draft').disabled, 'Save and publish must stay disabled after typing during save');
    await e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    e.app.showPublish(p.id);
    e.app.showRestore(p.id, {version: 1});
    assert(puts === 1 && !e.dialog.open, 'Repeated save, publish, or restore during save must not issue a competing mutation');
    wait.resolve({...p, content: 'B', draftRevision: 2}); await pending;
    assert(draft.content === 'C' && draft.savedContent === 'B' && draft.baseRevision === 2 && draft.dirty, 'Typing during save must survive and use the returned revision');
    assert(!e.app.packWrites.has(p.id) && !e.doc.getElementById('save-draft').disabled, 'Save lock must clear for the next intentional save');
    results.push('PASS: delayed save rejects duplicate writes and preserves further edits');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred();
    e.app.hooks({api: async path => path.endsWith('/draft') ? wait.promise : {packs: [p], devices: [], receipts: [], events: []}, refresh: async () => {}, render: () => {}});
    const pending = e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    const original = e.app.drafts.get(p.id); original.content = 'A'; original.dirty = false;
    await e.app.loadSnapshot();
    assert(e.app.drafts.get(p.id) === original, 'Polling cannot replace the edit object while saving, even after user reverts the text');
    assert(e.doc.getElementById('publish-draft').disabled, 'Reverting text during save cannot enable publish');
    wait.resolve({...p, content: 'B', draftRevision: 2}); await pending;
    assert(original.content === 'A' && original.savedContent === 'B' && original.dirty, 'The reverted edit must remain unsaved relative to the successful B save');
    results.push('PASS: heartbeat preserves edits reverted during an in-flight save');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred(); let publishes = 0;
    const draft = e.app.drafts.get(p.id); draft.content = 'A'; draft.dirty = false;
    e.app.hooks({api: async () => { publishes += 1; return wait.promise; }, refresh: async () => {}, render: () => {}});
    e.app.showPublish(p.id);
    const confirm = e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Publish version');
    const pending = confirm.trigger('click');
    draft.content = 'C'; draft.dirty = true;
    await e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    assert(publishes === 1 && e.app.packWrites.get(p.id) === 'publishing', 'A publish in flight must also block saves');
    wait.resolve({version: 1}); await pending;
    assert(!e.app.packWrites.has(p.id), 'Publish lock must clear');
    results.push('PASS: publication also blocks competing saves');
  }
  {
    const e = makeEnvironment(); const p = sourcePack(e); const job = sourceJob(p); const wait = deferred(); const messages = []; let submitted;
    e.respond(async (path, options) => {
      assert(path.endsWith('/publish') && options.method === 'POST', 'Source publication must use the existing explicit publish endpoint');
      submitted = JSON.parse(options.body); return wait.promise;
    });
    e.app.hooks({refresh: async () => {}, toast: message => messages.push(message)});
    e.app.showPublish(p.id);
    assert(e.dialog.textContent.includes('Git recording') && e.dialog.textContent.includes('Only this saved draft is submitted.'), 'Source publication confirmation must explain source recording and frozen saved content');
    const confirm = e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Publish version');
    const pending = confirm.trigger('click'); e.app.closeDialog();
    const editor = e.doc.getElementById('draft-editor'); editor.focus(); editor.value = 'Later local edit'; await editor.trigger('input');
    wait.resolve(response(202, {publication: job})); await pending;
    const draft = e.app.drafts.get(p.id);
    assert(submitted.baseRevision === 1 && draft.content === 'Later local edit' && draft.dirty, 'HTTP 202 publication must keep later local edits against the saved revision');
    assert(editor.isConnected && e.doc.activeElement === editor && editor.value === 'Later local edit', 'Queue acceptance cannot replace or refocus an active editor');
    assert(p.publishedVersion === 0 && p.source.pending === job.id && e.app.state.publications[0].id === job.id, 'HTTP 202 queues a job without claiming a published version');
    assert(!e.doc.getElementById('save-draft').disabled && e.doc.getElementById('publish-draft').disabled, 'An accepted queue must permit saving later edits while blocking another publication');
    assert(messages.length === 1 && messages[0] === 'Publication queued. Source is being recorded before delivery.', 'Queued publication must never report Version undefined or device installation');
    results.push('PASS: HTTP 202 queues source recording while preserving newer draft text and editor focus');
  }
  {
    const e = makeEnvironment(); const p = sourcePack(e); const job = sourceJob(p); let remote = {...p, source: {...p.source, pending: job.id, status: 'pending'}};
    e.app.hooks({api: async () => ({packs: [remote], devices: [], receipts: [], events: [], publications: [job]}), render: () => {}});
    await e.app.loadSnapshot();
    assert(e.doc.getElementById('publish-draft').disabled, 'A queued source publication observed on another client must block a duplicate publish');
    e.app.showPublish(p.id); assert(!e.dialog.open, 'Pending source ownership must also guard direct publish invocation');
    e.app.showRestore(p.id, {version: 1}); assert(e.dialog.open, 'A queued source publication must not block restoring a newer draft'); e.app.closeDialog();
    const editor = e.doc.getElementById('draft-editor'); editor.focus(); editor.value = 'Unsaved next version'; await editor.trigger('input');
    job.status = 'completed'; job.releaseVersion = 2; job.completedAt = '2026-10-12T00:00:30Z';
    remote = {...remote, publishedVersion: 2, publishedHash: job.hash, source: {...remote.source, pending: '', status: 'idle', proof: {...remote.source.proof, commit: 'd'.repeat(40), hash: job.hash}}};
    await e.app.loadSnapshot();
    const text = e.doc.getElementById('pack-source').textContent;
    assert(e.app.drafts.get(p.id).content === 'Unsaved next version' && e.doc.activeElement === editor && editor.isConnected, 'Worker completion polling must preserve next-version edits and focus');
    assert(text.includes('Published in Hub') && text.includes('Hub version 2') && text.includes('Check device coverage for installation.'), 'Worker completion must distinguish Hub publication from device installation');
    assert(text.includes('Source v6') && text.includes('Git commit dddddddddddd'), 'The source version and updated Git proof must be distinct from the Hub pack version');
    assert(!e.doc.getElementById('save-draft').disabled, 'The newer local draft remains saveable after source publication completes');
    results.push('PASS: queued and completed source snapshots update proof without replacing newer edits or claiming installation');
  }
  {
    const e = makeEnvironment(); const p = sourcePack(e); const job = sourceJob(p, 'failed'); job.error = '/private/internal-worker-detail';
    let remote = {...p, source: {...p.source, pending: job.id, status: 'failed', error: 'source_publish_failed'}};
    e.app.hooks({api: async () => ({packs: [remote], devices: [], receipts: [], events: [], publications: [job]}), render: () => {}});
    await e.app.loadSnapshot();
    const failed = e.doc.getElementById('pack-source').textContent;
    assert(failed.includes('This saved request remains available for retry.') && !failed.includes(job.error), 'Failed source jobs must preserve the frozen request with generic errors without promising an external worker retry');
    assert(e.doc.getElementById('publish-draft').disabled, 'A retryable failed job still owns its pending publication slot');
    job.status = 'rejected'; job.error = 'source_content_rejected';
    remote = {...remote, source: {...remote.source, pending: '', error: 'source_content_rejected'}}; await e.app.loadSnapshot();
    assert(!e.doc.getElementById('publish-draft').disabled && e.doc.getElementById('pack-source').textContent.includes('Edit and save a corrected draft'), 'Terminal source rejection must release publish ownership and explain how to correct the draft');
    e.app.showPublish(p.id); assert(e.dialog.open, 'A source rejection must permit a later explicit publication');
    results.push('PASS: failed source jobs retain their retry slot, while rejected jobs allow a corrected publication');
  }
  {
    const e = makeEnvironment(); const p = sourcePack(e); let attempts = 0; const messages = [];
    e.respond(async () => {
      attempts += 1;
      return attempts === 1 ? response(503, {error: 'Publication could not be queued. Try again after checking status.'}) : response(202, {publication: sourceJob(p)});
    });
    e.app.hooks({refresh: async () => {}, toast: message => messages.push(message), showError: (error, target) => { target.textContent = error.message; }});
    e.app.showPublish(p.id);
    const confirm = e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Publish version');
    await confirm.trigger('click');
    assert(e.dialog.open && !confirm.disabled && !e.app.packWrites.has(p.id), 'An unaccepted request failure must keep its confirmation available for an explicit retry');
    assert(e.dialog.textContent.includes('Publication could not be queued.') && !messages.length, 'A failed request must show its error without a success notification');
    await confirm.trigger('click');
    assert(attempts === 2 && !e.dialog.open && messages[0]?.startsWith('Publication queued.'), 'Only another explicit click may retry and report an accepted queue');
    results.push('PASS: an unaccepted source request stays reviewable and can be retried explicitly');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const draft = e.app.drafts.get(p.id); draft.content = 'A'; draft.dirty = false; const messages = [];
    e.respond(async () => response(200, {version: 1}));
    e.app.hooks({refresh: async () => {}, toast: message => messages.push(message)});
    e.app.showPublish(p.id); await e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Publish version').trigger('click');
    assert(messages[0] === 'Version 1 published. Installation is tracked separately.' && !e.app.state.publications.length, 'Ordinary packs must retain their direct immutable publication flow');
    results.push('PASS: ordinary HTTP 200 publication retains its existing behavior');
  }
  for (const finalText of ['C', 'A']) {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred(); let mutations = 0;
    const draft = e.app.drafts.get(p.id); draft.content = 'A'; draft.dirty = false;
    e.app.renderEditor(e.doc.getElementById('content'), p);
    e.app.hooks({api: async path => {
      if (path.endsWith('/restore')) { mutations += 1; return wait.promise; }
      assert(path === '/api/state', 'A pending restore must block competing draft writes');
      return {packs: [p], devices: [], receipts: [], events: []};
    }, refresh: async () => {}, render: () => {}});
    e.app.showRestore(p.id, {version: 1});
    const confirm = e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Restore to draft');
    const pending = confirm.trigger('click');
    await confirm.trigger('click');
    e.app.closeDialog();
    const editor = e.doc.getElementById('draft-editor'); editor.focus();
    editor.value = 'C'; await editor.trigger('input');
    editor.value = finalText; await editor.trigger('input');
    await e.app.loadSnapshot();
    assert(e.app.drafts.get(p.id) === draft, 'Polling cannot replace a draft while restoration is pending');
    assert(e.doc.getElementById('save-draft').disabled && e.doc.getElementById('publish-draft').disabled, 'Typing during restore cannot enable competing writes');
    await e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    e.app.showPublish(p.id); e.app.showRestore(p.id, {version: 1});
    assert(mutations === 1 && !e.dialog.open, 'A restore in flight must block save, publish, and another restore');
    wait.resolve({...p, content: 'Historical', draftRevision: 2}); await pending;
    assert(draft.content === finalText && draft.savedContent === 'Historical' && draft.baseRevision === 2 && draft.dirty, 'Restoration must keep later edits, including edits reverted to the original text, against the restored Hub baseline');
    assert(editor.value === finalText && e.doc.activeElement === editor, 'Restore completion must preserve the active editor and its text');
    assert(!e.app.packWrites.has(p.id) && !e.doc.getElementById('save-draft').disabled, 'The restored draft must allow the next intentional save');
    results.push(`PASS: delayed restore blocks competing writes and preserves ${finalText === 'A' ? 'reverted' : 'newer'} local edits`);
  }
  {
    const e = makeEnvironment(); const p = setupPack(e);
    const draft = e.app.drafts.get(p.id); draft.content = 'A'; draft.dirty = false;
    e.app.renderEditor(e.doc.getElementById('content'), p);
    e.app.hooks({api: async () => ({...p, content: 'Historical', draftRevision: 2}), refresh: async () => {}, render: () => {}});
    e.app.showRestore(p.id, {version: 1});
    await e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Restore to draft').trigger('click');
    assert(!draft.dirty && draft.content === 'Historical' && e.doc.getElementById('draft-editor').value === 'Historical', 'A restore without subsequent edits must display the restored saved content');
    assert(!e.dialog.open && !e.app.packWrites.has(p.id), 'Successful restore must close its dialog and release its lock');
    results.push('PASS: an untouched draft adopts restored history without publishing');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); let remote = {...p};
    e.doc.getElementById('content').append(e.app.renderPolicy(p));
    const elements = e.descendants(e.doc.getElementById('content'));
    const mode = elements.find(el => el.tagName === 'SELECT');
    const interval = elements.find(el => el.type === 'number');
    const paused = elements.find(el => el.type === 'checkbox');
    const sync = elements.find(el => el.tagName === 'BUTTON' && el.textContent.includes('Sync now'));
    e.app.hooks({api: async () => ({packs: [remote], devices: [], receipts: [], events: []}), render: () => {}});
    assert(sync.disabled, 'A pack without a release cannot request sync');
    interval.focus(); remote = {...p, mode: 'auto', intervalSeconds: 120, paused: true, publishedVersion: 1};
    await e.app.loadSnapshot();
    assert(mode.value === 'auto' && Number(interval.value) === 120 && paused.checked && !interval.disabled && !sync.disabled, 'Polling must update clean policy controls and enable sync after publishing elsewhere');
    assert(e.doc.activeElement === interval && interval.isConnected, 'Policy refresh cannot replace or refocus an active control');
    interval.value = '240'; await interval.trigger('input');
    mode.value = 'manual'; await mode.trigger('change'); paused.checked = false; await paused.trigger('change');
    remote = {...remote, intervalSeconds: 300}; await e.app.loadSnapshot();
    assert(mode.value === 'manual' && Number(interval.value) === 240 && !paused.checked && interval.disabled, 'Polling must preserve all unsaved policy fields');
    assert(e.doc.getElementById('policy-observed').textContent.includes('paused'), 'Observed delivery policy must still report fresh Hub state while local controls are dirty');
    assert(e.doc.activeElement === interval, 'Dirty policy polling cannot steal focus');
    results.push('PASS: policy heartbeat updates clean controls and sync availability without overwriting dirty controls or focus');
  }
  {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred(); let patches = 0; let remote = {...p};
    e.doc.getElementById('content').append(e.app.renderPolicy(p));
    const elements = e.descendants(e.doc.getElementById('content'));
    const form = elements.find(el => el.tagName === 'FORM');
    const mode = elements.find(el => el.tagName === 'SELECT');
    const interval = elements.find(el => el.type === 'number');
    const save = elements.find(el => el.tagName === 'BUTTON' && el.textContent === 'Save policy');
    const status = elements.find(el => el.attributes.role === 'status');
    e.app.hooks({api: async (path, method, body) => {
      if (path.endsWith('/policy')) { patches += 1; if (patches === 1) return wait.promise; remote = {...remote, ...body}; return remote; }
      return {packs: [remote], devices: [], receipts: [], events: []};
    }, render: () => {}});
    mode.value = 'auto'; await mode.trigger('change'); interval.value = '120'; await interval.trigger('input');
    const pending = form.trigger('submit');
    interval.value = '240'; await interval.trigger('input'); await e.app.loadSnapshot(); await form.trigger('submit');
    assert(save.disabled && patches === 1 && Number(interval.value) === 240, 'Policy edits during a pending save must survive polling and cannot issue a duplicate save');
    remote = {...p, mode: 'auto', intervalSeconds: 120}; wait.resolve(remote); await pending;
    assert(Number(interval.value) === 240 && status.textContent === 'Unsaved policy changes.' && !save.disabled, 'Policy save response must retain newer edits against the returned saved baseline');
    await form.trigger('submit');
    assert(patches === 2 && !status.textContent && Number(interval.value) === 240, 'Explicitly saving the remaining policy edit must establish a clean baseline');
    remote = {...remote, intervalSeconds: 360}; await e.app.loadSnapshot();
    assert(Number(interval.value) === 360 && !status.textContent, 'A policy saved cleanly must resume following later Hub updates');
    results.push('PASS: policy save protects later edits, blocks duplicate submissions, and resets the saved baseline');
  }
  {
    const e = makeEnvironment(); let requests = 0;
    e.respond(async () => { requests += 1; throw new Error('Invalid input must not reach the Hub'); });
    e.app.renderLogin();
    const form = e.descendants(e.doc.body).find(el => el.tagName === 'FORM');
    const token = e.descendants(form).find(el => el.tagName === 'INPUT');
    token.value = 'ssh example cat admin-token | pbcopy';
    await form.trigger('submit');
    const error = e.descendants(form).find(el => el.className === 'login-error');
    assert(requests === 0 && error.attributes.role === 'alert' && error.textContent.includes('64-character administrator token'), 'Pasted commands must explain the required credential without sending a login request');
    assert(e.doc.activeElement === token && token.value.includes('ssh'), 'Invalid pasted input must remain available to replace in the focused field');
    results.push('PASS: pasted terminal commands are rejected locally with a visible credential hint');
  }
  {
    const e = makeEnvironment(); let sent;
    e.respond(async (path, options) => {
      assert(path === '/api/login', 'Rejected credentials must not request authenticated state');
      sent = JSON.parse(options.body).token;
      return response(401, {error: 'authentication required'});
    });
    e.app.renderLogin();
    const form = e.descendants(e.doc.body).find(el => el.tagName === 'FORM');
    const token = e.descendants(form).find(el => el.tagName === 'INPUT');
    const submit = e.descendants(form).find(el => el.tagName === 'BUTTON');
    token.value = '  ' + 'a'.repeat(64) + '\n';
    await form.trigger('submit');
    const error = e.descendants(form).find(el => el.className === 'login-error');
    assert(sent === 'a'.repeat(64) && error.textContent.includes('token was not accepted'), 'A server rejection must explain the credential mismatch after removing incidental surrounding whitespace');
    assert(!error.textContent.includes(sent) && token.value === '' && e.doc.activeElement === token && !submit.disabled, 'Rejected credentials must be cleared, remain secret, and permit another attempt');
    results.push('PASS: rejected administrator tokens show a useful error and permit a safe retry');
  }
  {
    const e = makeEnvironment(); const paths = [];
    e.respond(async path => {
      paths.push(path);
      return path === '/api/login' ? response(200, {ok: true}) : response(401, {error: 'authentication required'});
    });
    e.app.renderLogin();
    const form = e.descendants(e.doc.body).find(el => el.tagName === 'FORM');
    e.descendants(form).find(el => el.tagName === 'INPUT').value = 'a'.repeat(64);
    await form.trigger('submit');
    const error = e.descendants(e.doc.body).find(el => el.className === 'login-error');
    assert(paths.join(',') === '/api/login,/api/state' && !form.isConnected, 'An accepted login must verify its session before opening the workspace');
    assert(error?.attributes.role === 'alert' && error.textContent.includes('token was accepted') && error.textContent.includes('Allow cookies'), 'A rejected browser session must explain the failure rather than silently resetting login');
    results.push('PASS: accepted credentials with an unavailable browser session show a visible recovery hint');
  }
  {
    const e = makeEnvironment(); e.app.renderLogin();
    const main = e.doc.getElementById('main');
    assert(main?.tagName === 'MAIN' && main.tabIndex === -1 && main.isConnected, 'The login screen must provide the focusable main landmark targeted by the persistent skip link');
    assert(e.descendants(main).some(el => el.tagName === 'FORM'), 'The login form must be contained by the main landmark');
    results.push('PASS: the login skip link has a focusable main landmark containing the form');
  }
  {
    const e = makeEnvironment(); ['pack-a', 'pack-b', 'unrelated'].forEach(id => setupPack(e, id));
    e.app.context('devices', 'unrelated');
    let submitted;
    e.app.hooks({api: async (path, method, body) => { submitted = body; return {token: 'synthetic-test-token', expiresAt: '2030-01-01T00:00:00Z'}; }, refresh: async () => {}});
    e.app.showEnrollment(['pack-a', 'pack-b']);
    await e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'Create enrollment token').trigger('click');
    await e.descendants(e.dialog).find(el => el.attributes['aria-label'] === 'Copy enroll command').trigger('click');
    const enrollmentCommand = e.descendants(e.dialog).find(el => el.tagName === 'PRE').textContent;
    assert(e.clipboard.writes[0] === enrollmentCommand && enrollmentCommand.includes('--token-file /private/path/enrollment-token') && !enrollmentCommand.includes('synthetic-test-token'), 'Enrollment copy must contain only the command and token-file placeholder, never the displayed credential');
    await e.descendants(e.dialog).find(el => el.tagName === 'BUTTON' && el.textContent === 'View binding guide').trigger('click');
    assert(submitted.packIds.join(',') === 'pack-a,pack-b', 'Enrollment must retain the selected scope');
    const text = e.dialog.textContent;
    assert(text.includes('--pack pack-a') && text.includes('--pack pack-b') && !text.includes('--pack unrelated'), 'Binding guide must use enrollment scope, not previously opened pack');
    assert(!text.includes('PACK_ID'), 'Known IDs should be filled into binding commands');
    const commands = e.descendants(e.dialog).filter(el => el.tagName === 'PRE').map(el => el.textContent);
    assert(commands.every(command => command.split('\n').filter(line => /rulesend agent/.test(line)).every(line => line.startsWith('./rulesend agent'))), 'Source-build commands must consistently use ./rulesend');
    assert(text.includes('/project-1/AGENTS.md') && text.includes('/project-2/AGENTS.md'), 'Different packs must show different target placeholders');
    results.push('PASS: enrollment-to-guide uses exact multi-pack scope and source-build commands');
    const previewIndex = commands.findIndex(command => command.includes('agent preview'));
    const applyIndex = commands.findIndex(command => command.includes('agent once'));
    assert(previewIndex >= 0 && applyIndex > previewIndex, 'The guide must show read-only preview before applying');
    assert(!commands[previewIndex].includes('agent once') && commands.every(command => command.match(/\.\/rulesend agent /g).length === 1), 'Each copyable snippet must contain one intentional Agent command');
    assert(text.includes('Stop Run first.') && text.includes('Local file contents stay on the device.') && text.includes('No rule file is changed or marked installed.'), 'Preview copy must distinguish inspection, local contents, and installed status');
    const copyButtons = e.descendants(e.dialog).filter(el => /^Copy .* command$/.test(el.attributes['aria-label'] || ''));
    assert(copyButtons.length === commands.length, 'Every generated command needs its own copy button');
    await copyButtons.find(el => el.attributes['aria-label'] === 'Copy bind pack-b command').trigger('click');
    const copiedBind = e.clipboard.writes[e.clipboard.writes.length - 1];
    assert(copiedBind === commands.find(command => command.includes('--pack pack-b')) && !copiedBind.includes('--pack pack-a') && !copiedBind.includes('synthetic-test-token'), 'Copying pack B must copy only pack B’s command without the enrollment secret');
    results.push('PASS: preview precedes apply and each scoped snippet copies only its own command');
    e.clipboard.fail = true;
    await copyButtons.find(el => el.attributes['aria-label'] === 'Copy bind pack-a command').trigger('click');
    const fallback = e.descendants(e.dialog).find(el => el.tagName === 'TEXTAREA');
    assert(fallback?.readOnly && fallback.selected && e.doc.activeElement === fallback, 'A blocked clipboard must expose and select a read-only manual copy field');
    assert(fallback.value === commands.find(command => command.includes('--pack pack-a')) && !fallback.value.includes('synthetic-test-token'), 'Manual fallback must contain the requested scoped command, without secret content');
    results.push('PASS: clipboard denial selects a safe scoped command for manual copying');
  }
  {
    const e = makeEnvironment(); const container = e.mount('refresh-region');
    const oldButton = new e.Element('button'); oldButton.dataset.focusKey = 'device-revoke:stable-id';
    const oldTable = new e.Element('div'); oldTable.dataset.scrollKey = 'devices'; oldTable.scrollLeft = 240;
    container.append(oldTable, oldButton); oldButton.focus();
    let nextButton, nextTable;
    e.app.preserveInteraction(container, () => {
      nextButton = new e.Element('button'); nextButton.dataset.focusKey = oldButton.dataset.focusKey;
      nextTable = new e.Element('div'); nextTable.dataset.scrollKey = 'devices';
      container.replaceChildren(new e.Element('button'), nextButton, nextTable);
    });
    assert(e.doc.activeElement === nextButton && nextButton.focusOptions.preventScroll, 'Polling must restore the same device action by identity, despite changed ordering');
    assert(nextTable.scrollLeft === 240, 'Polling must retain horizontal table position');
    e.app.preserveInteraction(container, () => { const wrong = new e.Element('button'); wrong.dataset.focusKey = 'device-revoke:another-id'; container.replaceChildren(wrong); });
    assert(e.doc.activeElement === nextButton, 'A removed action must not focus a different item at the same index');
    results.push('PASS: focus restores by identity and horizontal scroll survives refresh');
  }
  return {kind: 'deterministic DOM-model checks; not browser/visual verification', checks: results};
}

// Optional command-line entry point: node tests/ui-regression.cjs
if (typeof module !== 'undefined' && require.main === module) {
  const {readFileSync} = require('node:fs');
  const {join} = require('node:path');
  const source = readFileSync(join(__dirname, '../internal/hub/web/app.js'), 'utf8');
  runRuleSendChecks(source).then(result => {
    console.log(result.kind);
    console.log(result.checks.join('\n'));
  }).catch(error => { console.error(error.message); process.exitCode = 1; });
}
