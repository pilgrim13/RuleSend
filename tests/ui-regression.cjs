/* Deterministic DOM-model regression checks. Not a browser or visual test.
 * Usage in a JS runtime: await runRuleSendChecks(appSourceString).
 * No filesystem, network, DOM package, or Node dependency is required. */
async function runRuleSendChecks(source) {
  const results = [];
  function assert(condition, message) { if (!condition) throw new Error(message); }
  function makeEnvironment() {
    const doc = {activeElement: null};
    const clipboard = {writes: [], fail: false, async writeText(text) { if (this.fail) throw new Error('Clipboard denied'); this.writes.push(text); }};
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
      return {state, drafts, packWrites, saveDraft, showPublish, showEnrollment, showGuide, refreshEditorStatus, loadSnapshot, preserveInteraction,
        context(nextView, id) { view = nextView; selectedPack = id; },
        hooks(values) {
          if (values.api) api = values.api;
          if (values.refresh) refresh = values.refresh;
          if (values.render) { renderShell = values.render; renderView = values.render; renderConnection = values.render; }
          toast = () => {}; showError = error => { throw error; };
        }
      };
    })();`;
    const app = new Function('document', 'window', 'location', 'navigator', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'TextEncoder', instrumented)(doc, {}, {origin: 'http://127.0.0.1:8787', hostname: '127.0.0.1'}, {clipboard}, () => 0, () => {}, () => 0, () => {}, class { encode(text) { return {length: text.length}; } });
    function descendants(el) { return [el, ...el.children.flatMap(descendants)]; }
    return {app, doc, dialog, Element, mount, descendants, clipboard};
  }
  function setupPack(env, id = 'pack-a') {
    const p = {id, name: id, content: 'A', draftRevision: 1, publishedVersion: 0, mode: 'manual', intervalSeconds: 60, paused: false};
    env.app.state.packs.push(p);
    env.app.drafts.set(id, {content: 'B', savedContent: 'A', baseRevision: 1, dirty: true});
    env.app.context('packs', id); return p;
  }
  function deferred() { let resolve; const promise = new Promise(r => resolve = r); return {promise, resolve}; }

  {
    const e = makeEnvironment(); const p = setupPack(e); const wait = deferred(); let puts = 0;
    e.app.hooks({api: async () => { puts += 1; return wait.promise; }, refresh: async () => {}});
    const pending = e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    const draft = e.app.drafts.get(p.id); draft.content = 'C'; draft.dirty = true;
    e.app.refreshEditorStatus();
    assert(e.doc.getElementById('save-draft').disabled && e.doc.getElementById('publish-draft').disabled, 'Save and publish must stay disabled after typing during save');
    await e.app.saveDraft(p.id, e.doc.getElementById('save-draft'));
    e.app.showPublish(p.id);
    assert(puts === 1 && !e.dialog.open, 'Repeated save or publish during save must not issue a competing mutation');
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
