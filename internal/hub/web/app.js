/* RuleSend's browser keeps credentials and unpublished edits out of persistent storage. */
'use strict';

(() => {
  const app = document.getElementById('app');
  const dialog = document.getElementById('dialog');
  const notifications = document.getElementById('notifications');
  const state = {packs: [], devices: [], receipts: [], events: []};
  const drafts = new Map();
  const details = new Map();
  let view = 'overview';
  let selectedPack = null;
  let packTab = 'write';
  let authenticated = false;
  let authGeneration = 0;
  let refreshPromise = null;
  let lastRefreshed = null;
  let connectionError = false;
  let activeTimer;
  const STALE_AFTER = 60;

  function node(tag, classes, text) {
    const element = document.createElement(tag);
    if (classes) element.className = classes;
    if (text !== undefined && text !== null) element.textContent = String(text);
    return element;
  }
  function append(parent, ...children) {
    children.flat().filter(Boolean).forEach(child => parent.append(child));
    return parent;
  }
  function button(text, handler, classes = '') {
    const element = node('button', `button ${classes}`, text);
    element.type = 'button';
    if (handler) element.addEventListener('click', handler);
    return element;
  }
  function badge(text, tone = 'neutral') { return node('span', `badge ${tone}`, text); }
  function textLink(text, handler) { return button(text, handler, 'ghost compact'); }
  function date(value) {
    const d = new Date(value);
    return Number.isFinite(d.getTime()) && d.getUTCFullYear() > 1970 ? d : null;
  }
  function ageSeconds(value) {
    const d = date(value);
    return d ? Math.max(0, (Date.now() - d.getTime()) / 1000) : Infinity;
  }
  function ago(value) {
    const seconds = ageSeconds(value);
    if (!Number.isFinite(seconds)) return 'Never seen';
    if (seconds < 10) return 'Just now';
    if (seconds < 60) return `${Math.floor(seconds)}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
  }
  function fullDate(value) { return date(value)?.toLocaleString() || 'Not yet recorded'; }
  function seen(value) {
    const element = node('time', '', ago(value));
    if (date(value)) element.dateTime = value;
    element.title = fullDate(value);
    return element;
  }
  function pack(id) { return state.packs.find(p => p.id === id); }
  function device(id) { return state.devices.find(d => d.id === id); }
  function fresh(d) { return !connectionError && d && !d.revoked && ageSeconds(d.lastSeen) <= STALE_AFTER; }
  function currentReceipt(r) {
    const p = pack(r.packId);
    return p && p.publishedVersion > 0 && r.status === 'applied' && r.version === p.publishedVersion && r.hash === p.publishedHash;
  }
  function verifiedCurrent(r) {
    // Agents recheck the file on every heartbeat, independently of apply policy.
    return currentReceipt(r) && fresh(device(r.deviceId)) && ageSeconds(r.checkedAt) <= STALE_AFTER;
  }
  function receiptState(r) {
    if (device(r.deviceId)?.revoked) return ['Access revoked', 'neutral'];
    if (r.status === 'conflict') return ['Local conflict', 'error'];
    if (r.status === 'error') return ['Needs attention', 'error'];
    if (!fresh(device(r.deviceId))) return [r.status === 'applied' ? 'Installed · stale' : 'Not recently seen', 'warning'];
    if (verifiedCurrent(r)) return ['Current file verified', 'good'];
    if (currentReceipt(r)) return ['Installed · check old', 'warning'];
    if (r.status === 'applied') return ['Earlier version', 'warning'];
    return ['Awaiting installation', 'neutral'];
  }
  function toast(message, error = false) {
    const el = append(node('div', `toast${error ? ' error' : ''}`), node('p', '', message));
    const close = button('×', () => el.remove(), 'ghost');
    close.setAttribute('aria-label', 'Dismiss notification');
    append(el, close); notifications.append(el);
    setTimeout(() => el.remove(), error ? 15000 : 6500);
  }
  async function api(path, method = 'GET', body) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    const options = {method, credentials: 'same-origin', cache: 'no-store', signal: controller.signal, headers: {'Accept': 'application/json'}};
    if (method !== 'GET') { options.headers['Content-Type'] = 'application/json'; options.body = JSON.stringify(body || {}); }
    let response;
    try { response = await fetch(path, options); }
    catch (_) { throw new Error('No response from the Hub. Your local draft is preserved; reconnect and check the latest state before retrying.'); }
    finally { clearTimeout(timeout); }
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(typeof result.error === 'string' ? result.error : 'The request could not be completed.');
      error.status = response.status;
      throw error;
    }
    return result;
  }
  function showError(error, target) {
    if (error.status === 401 && authenticated) {
      authenticated = false;
      closeDialog();
      renderLogin('Your session has expired. Sign in again to continue.');
      return;
    }
    if (target) { target.textContent = error.message; target.classList.remove('hidden'); }
    else toast(error.message, true);
  }
  async function busy(element, action) {
    if (element) element.disabled = true;
    try { return await action(); }
    finally { if (element?.isConnected) element.disabled = false; }
  }
  function field(label, input, hint) {
    const wrapper = node('div', 'field');
    const id = `field-${Math.random().toString(36).slice(2)}`;
    input.id = id;
    const labelEl = node('label', '', label); labelEl.htmlFor = id;
    append(wrapper, labelEl, input, hint ? node('span', 'field-hint', hint) : null);
    return wrapper;
  }
  function input(type = 'text', value = '') {
    const el = node('input'); el.type = type; el.value = value; return el;
  }
  function select(options, value) {
    const el = node('select');
    options.forEach(([key, label]) => { const option = node('option', '', label); option.value = key; el.append(option); });
    el.value = value; return el;
  }
  function note(text, tone = '') { return node('div', `notice ${tone}`, text); }
  function empty(title, message, action, compact = false) {
    return append(node('div', `empty${compact ? ' compact' : ''}`), node('div', 'empty-visual', '↗'), node('h3', '', title), node('p', '', message), action);
  }
  function panel(title, subtitle, action) {
    const el = node('section', 'panel');
    const heading = append(node('div'), node('h2', '', title), subtitle ? node('p', '', subtitle) : null);
    el.append(append(node('div', 'panel-head'), heading, action));
    return el;
  }
  function openDialog(title, wide = false) {
    if (dialog.open) dialog.close();
    dialog.replaceChildren(); dialog.className = wide ? 'wide' : '';
    const close = button('×', closeDialog, 'dialog-close'); close.setAttribute('aria-label', 'Close dialog');
    const heading = node('h2', '', title); heading.id = 'dialog-title';
    const body = node('div', 'dialog-body'); const footer = node('div', 'dialog-actions');
    append(dialog, append(node('div', 'dialog-head'), heading, close), body, footer);
    dialog.showModal();
    return {body, footer};
  }
  function closeDialog() { if (dialog.open) dialog.close(); dialog.replaceChildren(); }
  dialog.addEventListener('cancel', event => { event.preventDefault(); closeDialog(); });
  function brand() { return append(node('div', 'brand'), node('span', 'brand-mark', '↗'), node('span', 'brand-word', 'RuleSend')); }

  function renderLogin(message = '') {
    authGeneration += 1;
    clearInterval(activeTimer);
    const screen = node('div', 'login-screen');
    const story = node('div', 'login-story');
    const words = node('div');
    append(words, node('div', 'eyebrow', 'A little less drift. A lot more clarity.'));
    const headline = node('h1'); append(headline, 'Your AI rules.', node('br'), node('span', '', 'Every device.'));
    append(words, headline, node('p', '', 'One place to write your rules, publish a version, and see which devices have actually installed it.'));
    const art = node('div', 'connection-art'); art.setAttribute('aria-hidden', 'true');
    append(art, node('div', 'art-node hub', 'Your rules'), node('div', 'art-line'), append(node('div', 'art-stack'), node('div', 'art-node', 'Codex'), node('div', 'art-node', 'Claude Code')));
    words.append(art);
    append(story, brand(), words, append(node('div', 'login-notes'), node('span', '', 'Self-hosted'), node('span', '', 'Open source'), node('span', '', 'Your rules stay yours')));
    const form = node('form', 'login-form');
    const token = input('password'); token.required = true; token.autocomplete = 'off'; token.spellcheck = false;
    const error = node('p', 'login-error', message); error.setAttribute('role', 'alert');
    const submit = button('Open your workspace →', null, 'primary full'); submit.type = 'submit';
    append(form, node('div', 'eyebrow', 'Welcome to your Hub'), node('h2', '', 'Make yourself at home.'), node('p', '', 'Sign in with the administrator token created when you initialized this Hub.'), field('Administrator token', token, 'Paste the contents of your Hub’s admin-token file.'), error, submit, node('p', 'login-note', 'Your token is exchanged for a private session cookie. It is never saved in browser storage. Keep the token file private.'));
    form.addEventListener('submit', async event => {
      event.preventDefault(); error.textContent = '';
      const secret = token.value; token.value = '';
      await busy(submit, async () => {
        try { await api('/api/login', 'POST', {token: secret}); authenticated = true; await refresh(true); }
        catch (err) { error.textContent = err.message; token.focus(); }
      });
    });
    append(screen, story, append(node('div', 'login-form-wrap'), form)); app.replaceChildren(screen);
  }

  function renderShell() {
    const shell = node('div', 'shell');
    const sidebar = node('aside', 'sidebar');
    append(sidebar, append(node('div'), brand(), node('p', 'brand-sub', 'Your rules, in sync.')));
    const nav = node('nav', 'nav'); nav.setAttribute('aria-label', 'Main navigation');
    const navItems = [['overview', '◫', 'Overview'], ['packs', '≡', 'Rule packs'], ['devices', '▣', 'Devices'], ['activity', '◷', 'Activity']];
    navItems.forEach(([id, icon, label]) => {
      const item = button('', () => navigate(id), 'nav-item');
      item.dataset.view = id; item.title = label; item.setAttribute('aria-label', label);
      append(item, node('span', 'nav-icon', icon), node('span', 'nav-label', label)); nav.append(item);
    });
    sidebar.append(nav);
    append(sidebar, append(node('div', 'sidebar-bottom'), append(node('div', 'local-card'), node('strong', '', '↗  A workspace you own'), node('p', '', 'One Hub. Your devices. No hidden cloud.')), append(node('div', 'sidebar-foot'), node('span', '', 'RuleSend / alpha'), node('span', '', 'Self-hosted'))));
    const main = node('main', 'main'); main.id = 'main'; main.tabIndex = -1;
    const breadcrumb = node('div', 'breadcrumb'); breadcrumb.id = 'breadcrumb';
    const live = node('div', 'live-indicator'); live.id = 'hub-connection';
    const signout = textLink('Sign out', logout);
    append(main, append(node('header', 'topbar'), breadcrumb, append(node('div', 'topbar-right'), live, signout, node('div', 'avatar', 'Y'))));
    const content = node('div'); content.id = 'content'; main.append(content);
    const footer = append(node('footer', 'page-footer'), node('span', '', 'Built for clarity. File installation is verified separately from AI behavior.'), textLink('Agent setup guide ↗', showGuide));
    main.append(footer); append(shell, sidebar, main); app.replaceChildren(shell);
    renderConnection(); renderView();
    clearInterval(activeTimer); activeTimer = setInterval(() => refresh(false), 15000);
  }
  async function logout() {
    if ([...drafts.values()].some(d => d.dirty)) {
      const {body, footer} = openDialog('Sign out with unsaved changes?');
      body.append(node('p', '', 'Your unsaved edits are only in this browser tab. Save your draft before signing out if you want to keep them.'));
      append(footer, button('Keep editing', closeDialog), button('Discard edits & sign out', performLogout, 'danger')); return;
    }
    await performLogout();
  }
  async function performLogout() {
    try { await api('/api/logout', 'POST', {}); authenticated = false; drafts.clear(); details.clear(); Object.keys(state).forEach(k => state[k] = []); closeDialog(); renderLogin(); }
    catch (err) { showError(err); }
  }
  function renderConnection() {
    const el = document.getElementById('hub-connection'); if (!el) return;
    const dot = node('span', `dot${connectionError ? ' offline' : ''}`);
    el.replaceChildren();
    append(el, dot, node('span', '', connectionError ? 'Hub unreachable · data may be old' : 'Connected to your Hub'));
    el.title = lastRefreshed ? `Last checked ${fullDate(lastRefreshed)}` : 'Connecting';
  }
  async function refresh(initial = false) {
    const running = (refreshPromise || Promise.resolve()).catch(() => {}).then(() => loadSnapshot(initial));
    refreshPromise = running;
    try { await running; } finally { if (refreshPromise === running) refreshPromise = null; }
  }
  async function loadSnapshot(initial = false) {
    const generation = authGeneration;
    try {
      const result = await api('/api/state');
      if (generation !== authGeneration) return;
      Object.keys(state).forEach(key => state[key] = Array.isArray(result[key]) ? result[key] : []);
      state.packs.forEach(p => {
        const d = drafts.get(p.id);
        if (!d || !d.dirty) drafts.set(p.id, {content: p.content || '', savedContent: p.content || '', baseRevision: p.draftRevision, dirty: false});
      });
      authenticated = true; connectionError = false; lastRefreshed = new Date().toISOString();
      if (initial || !document.getElementById('content')) renderShell();
      else {
        renderConnection();
        if (view === 'packs' && selectedPack && pack(selectedPack)) refreshEditorStatus();
        else renderView();
      }
    } catch (error) {
      if (generation !== authGeneration) return;
      if (error.status === 401) {
        authenticated = false;
        closeDialog();
        renderLogin(initial ? '' : 'Your session has expired. Sign in to continue.');
      } else if (initial) {
        authenticated = false; renderLogin(error.message);
      } else {
        connectionError = true; renderConnection();
        if (view === 'packs' && selectedPack) refreshEditorStatus(); else renderView();
      }
    }
  }
  function navigate(next, packId) {
    view = next;
    if (packId) { selectedPack = packId; packTab = 'write'; }
    renderView();
  }
  function renderView() {
    const content = document.getElementById('content'); if (!content) return;
    const labels = {overview: 'Overview', packs: 'Rule packs', devices: 'Devices', activity: 'Activity'};
    const crumb = document.getElementById('breadcrumb'); crumb.replaceChildren(); append(crumb, 'Workspace', node('span', '', '  /  '), node('strong', '', labels[view]));
    document.querySelectorAll('[data-view]').forEach(el => {
      const active = el.dataset.view === view; el.classList.toggle('active', active);
      if (active) el.setAttribute('aria-current', 'page'); else el.removeAttribute('aria-current');
    });
    content.replaceChildren();
    ({overview: renderOverview, packs: renderPacks, devices: renderDevices, activity: renderActivity})[view](content);
  }
  function pageHead(title, subtitle, actions) {
    return append(node('div', 'page-head'), append(node('div'), node('div', 'eyebrow', 'YOUR WORKSPACE'), node('h1', '', title), node('p', 'subtitle', subtitle)), actions ? append(node('div', 'head-actions'), actions) : null);
  }
  function renderStats() {
    const receipts = state.receipts.filter(r => !device(r.deviceId)?.revoked);
    const current = receipts.filter(verifiedCurrent).length;
    const needs = receipts.filter(r => ['conflict', 'error'].includes(r.status)).length;
    const online = state.devices.filter(fresh).length;
    const values = [
      ['Rule packs', state.packs.length, `${state.packs.filter(p => p.publishedVersion > 0).length} with a published version`, '≡'],
      ['Devices online', online, `${state.devices.filter(d => !d.revoked).length} enrolled · seen in the last minute`, '▣'],
      ['Files current', current, `${receipts.length} reported targets · version + hash`, '✓'],
      ['Needs attention', needs, needs ? 'Review local conflicts or errors' : 'No reported conflicts or errors', '↗']
    ];
    const grid = node('div', 'stats');
    values.forEach(([label, value, hint, icon]) => grid.append(append(node('div', 'stat'), append(node('div', 'stat-top'), node('span', 'stat-label', label), node('span', 'stat-icon', icon)), node('div', 'stat-value', value), node('div', 'stat-note', hint))));
    return grid;
  }
  function renderOverview(content) {
    append(content, pageHead('Your rules. Everywhere you work.', 'Write once. Publish deliberately. Know what arrived.', [button('＋ Add device', showEnrollment), button('＋ New rule pack', showNewPack, 'primary')]), renderStats());
    const grid = node('div', 'overview-grid');
    const packsPanel = panel('Rule packs', 'Your shared instructions, with a clear version history.', textLink('View all →', () => navigate('packs')));
    if (state.packs.length) packsPanel.append(packRows(state.packs.slice(0, 4)));
    else packsPanel.append(empty('Start with your first rule pack', 'Add the instructions you want your AI tools to share. Your draft stays private until you publish.', button('Create a rule pack', showNewPack, 'primary')));
    const workflow = panel('A calmer way to keep in sync', 'A clear handoff, from draft to device.');
    const journey = node('div', 'panel-body journey');
    [['01', 'Write & review', 'Keep your shared rules in one Markdown draft.'], ['02', 'Publish a version', 'Send an immutable release to the devices you choose.'], ['03', 'Verify the file', 'See the version, hash, and last check from each target.']].forEach(([num, title, text]) => append(journey, append(node('div', 'step'), node('span', 'step-number', num), append(node('div'), node('h3', '', title), node('p', '', text)))));
    journey.append(note('File installed ≠ AI loaded. App loading and behavior remain unverified until they have their own evidence.'));
    workflow.append(journey); append(grid, packsPanel, workflow); content.append(grid);
    content.append(coveragePanel());
  }
  function packRows(items) {
    const list = node('div', 'pack-list');
    items.forEach(p => {
      const title = button(p.name, () => navigate('packs', p.id), 'text-button pack-name');
      const enrolled = state.devices.filter(d => !d.revoked && (d.packIds || []).includes(p.id)).length;
      const description = p.description || `${enrolled} scoped ${enrolled === 1 ? 'device' : 'devices'} · ${p.mode === 'manual' ? 'Manual delivery' : 'Automatic delivery'}`;
      append(list, append(node('div', 'pack-row'), node('div', 'pack-file', 'MD'), append(node('div', 'grow'), title, node('div', 'pack-detail', description)), badge(p.paused ? 'Paused' : p.publishedVersion ? `v${p.publishedVersion} published` : 'Draft only', p.paused ? 'warning' : p.publishedVersion ? 'good' : 'neutral')));
    }); return list;
  }
  function coveragePanel(filterId) {
    const panelEl = panel('Device coverage', 'Observed file installation, with separate AI verification.', textLink('Manage devices →', () => navigate('devices')));
    const rows = state.receipts.filter(r => !filterId || r.packId === filterId);
    const relevantDevices = state.devices.filter(d => !filterId || (d.packIds || []).includes(filterId));
    if (!rows.length) {
      panelEl.append(empty('No file checks yet', relevantDevices.length ? 'Devices are enrolled. Bind a local target and run the Agent to receive the first file check.' : 'Enroll a device, choose a local target, and let your Agent report what is installed.', button(relevantDevices.length ? 'Open setup guide' : 'Add your first device', relevantDevices.length ? showGuide : showEnrollment), true));
    } else {
      const table = node('table'); const head = node('tr');
      ['Device / target', 'Rule pack', 'File installation', 'AI loaded', 'Behavior', 'Last file check'].forEach(text => head.append(node('th', '', text)));
      append(table, append(node('thead'), head)); const body = node('tbody');
      rows.forEach(r => {
        const d = device(r.deviceId); const p = pack(r.packId); const [status, tone] = receiptState(r);
        const target = append(node('td'), node('span', 'cell-title', d?.name || 'Unknown device'), node('span', 'cell-sub', `${r.adapter || 'Markdown'} · ${r.targetLabel || 'Local target'}`));
        const rule = append(node('td'), node('span', 'cell-title', p?.name || r.packId), node('span', 'cell-sub', p?.publishedVersion ? `Target v${p.publishedVersion}` : 'No published version'));
        const installed = append(node('td'), badge(status, tone), node('span', 'cell-sub', r.version ? `Reported v${r.version} · ${String(r.hash || '').slice(0, 10)}` : 'No installed version'));
        if (r.error) installed.append(node('span', 'cell-sub', `${r.error} Check local Agent status for details.`));
        const check = append(node('td'), seen(r.checkedAt), node('span', 'cell-sub', `Device seen: ${ago(d?.lastSeen)}`));
        body.append(append(node('tr'), target, rule, installed, append(node('td'), badge('Unverified')), append(node('td'), badge('Unverified')), check));
      }); table.append(body); panelEl.append(append(node('div', 'table-wrap'), table));
    }
    panelEl.append(append(node('div', 'coverage-note'), node('strong', '', 'Honest by design'), node('span', '', 'A heartbeat confirms connection. A receipt confirms a file check. Neither proves an AI has read or followed it.')));
    return panelEl;
  }

  function renderPacks(content) {
    if (!selectedPack || !pack(selectedPack)) selectedPack = state.packs[0]?.id || null;
    const p = pack(selectedPack);
    if (!p) {
      append(content, pageHead('Your shared rulebook.', 'Small, focused packs. One source for the way you work.', button('＋ New rule pack', showNewPack, 'primary')), append(panel('Rule packs', 'Drafts stay drafts until you publish.'), empty('What should your AI know?', 'Start with your writing preferences, coding conventions, or project instructions.', button('Create a rule pack', showNewPack, 'primary')))); return;
    }
    const picker = select(state.packs.map(item => [item.id, item.name]), selectedPack); picker.className = 'editor-select'; picker.setAttribute('aria-label', 'Select a rule pack');
    picker.addEventListener('change', () => { selectedPack = picker.value; packTab = 'write'; renderView(); });
    append(content, pageHead(p.name, p.description || 'A shared rule pack for the way you work.', [picker, button('＋ New pack', showNewPack)]));
    const alert = node('div', 'hidden banner'); alert.id = 'draft-alert'; content.append(alert);
    const layout = node('div', 'draft-layout'); const main = node('div'); const editorPanel = node('section', 'panel');
    const tabs = node('div', 'tabs'); tabs.setAttribute('aria-label', 'Rule pack views');
    [['write', 'Write & preview'], ['history', 'Version history']].forEach(([key, title]) => { const b = button(title, () => { packTab = key; renderView(); }, `tab${packTab === key ? ' active' : ''}`); b.setAttribute('aria-pressed', String(packTab === key)); tabs.append(b); });
    const version = node('div'); version.id = 'pack-version';
    append(editorPanel, append(node('div', 'editor-header'), tabs, version));
    if (packTab === 'write') renderEditor(editorPanel, p); else renderHistory(editorPanel, p);
    main.append(editorPanel);
    const coverage = node('div', 'list-space'); coverage.id = 'pack-coverage'; coverage.append(coveragePanel(p.id)); main.append(coverage);
    append(layout, main, renderPolicy(p)); content.append(layout); refreshEditorStatus();
  }
  function renderEditor(parent, p) {
    const draft = drafts.get(p.id);
    const source = node('textarea', 'editor'); source.value = draft.content; source.spellcheck = false; source.setAttribute('aria-label', 'Markdown rule draft'); source.id = 'draft-editor';
    source.placeholder = '# How I like to work\n\nWrite the instructions your tools should share.';
    const preview = node('div', 'preview'); preview.id = 'draft-preview'; preview.setAttribute('aria-label', 'Markdown preview'); renderMarkdown(preview, draft.content);
    const grid = node('div', 'editor-grid');
    append(grid, append(node('div', 'editor-column'), node('div', 'column-label', 'Markdown · draft'), source), append(node('div', 'editor-column'), node('div', 'column-label', 'Preview · raw HTML is disabled'), preview)); parent.append(grid);
    source.addEventListener('input', () => {
      const d = drafts.get(p.id); d.content = source.value; d.dirty = d.content !== d.savedContent;
      renderMarkdown(preview, d.content); refreshEditorStatus();
    });
    const status = node('span'); status.id = 'draft-save-status';
    const save = button('Save draft', () => saveDraft(p.id, save)); save.id = 'save-draft';
    const publish = button('Publish version ↗', () => showPublish(p.id), 'primary'); publish.id = 'publish-draft';
    append(parent, append(node('div', 'editor-footer'), status, append(node('div', 'actions'), button('Compare', () => showCompare(p.id), 'ghost compact'), save, publish)));
  }
  function refreshEditorStatus() {
    if (view !== 'packs') return;
    const p = pack(selectedPack); const d = drafts.get(selectedPack); if (!p || !d) return;
    const version = document.getElementById('pack-version');
    if (version) version.replaceChildren(badge(p.publishedVersion ? `v${p.publishedVersion} published` : 'Not published yet', p.publishedVersion ? 'good' : 'neutral'));
    const status = document.getElementById('draft-save-status');
    if (status) status.textContent = `${d.dirty ? 'Unsaved changes' : 'Draft saved'} · ${new TextEncoder().encode(d.content).length.toLocaleString()} bytes · revision ${d.baseRevision}`;
    const source = document.getElementById('draft-editor');
    if (source && !d.dirty && source.value !== d.content) {
      source.value = d.content;
      renderMarkdown(document.getElementById('draft-preview'), d.content);
    }
    const save = document.getElementById('save-draft'); if (save) save.disabled = !d.dirty;
    const publish = document.getElementById('publish-draft'); if (publish) publish.disabled = d.dirty;
    const alert = document.getElementById('draft-alert');
    if (alert) {
      alert.replaceChildren();
      if (d.baseRevision !== p.draftRevision && d.dirty) {
        alert.className = 'notice warning banner';
        append(alert, node('span', '', 'This draft changed elsewhere. Your local edits are preserved. '), textLink('Review both drafts', () => showConflict(p.id)));
      } else { alert.className = 'hidden banner'; }
    }
    const observed = document.getElementById('policy-observed');
    if (observed) observed.textContent = p.paused ? 'Delivery is paused. Sync now will not resume it.' : !p.publishedVersion ? 'Publish a version before a device can install this pack.' : p.mode === 'manual' ? 'Devices wait for Sync now or a local Agent once command.' : `Agents check this pack every ${p.intervalSeconds} seconds while connected.`;
    const coverage = document.getElementById('pack-coverage');
    if (coverage && document.activeElement?.id !== 'draft-editor') coverage.replaceChildren(coveragePanel(p.id));
  }
  async function saveDraft(id, element) {
    const d = drafts.get(id); const submitted = d.content; const baseRevision = d.baseRevision;
    await busy(element, async () => {
      try {
        const result = await api(`/api/packs/${encodeURIComponent(id)}/draft`, 'PUT', {baseRevision, content: submitted});
        const current = drafts.get(id); current.baseRevision = result.draftRevision; current.savedContent = submitted; current.dirty = current.content !== submitted;
        const index = state.packs.findIndex(p => p.id === id); if (index >= 0) state.packs[index] = result;
        details.delete(id); refreshEditorStatus(); toast('Draft saved. Nothing has been published.'); await refresh();
      } catch (err) {
        if (err.status === 409) { d.dirty = true; await refresh(); refreshEditorStatus(); toast('This draft changed elsewhere. Review both versions before saving.', true); }
        else showError(err);
      }
    });
    refreshEditorStatus();
  }
  async function getDetail(id) {
    const result = await api(`/api/packs/${encodeURIComponent(id)}`); details.set(id, result); return result;
  }
  async function renderHistory(parent, p) {
    const holder = append(node('div'), node('div', 'loading', 'Loading published versions…')); parent.append(holder);
    try {
      const result = await getDetail(p.id); if (!holder.isConnected) return; holder.replaceChildren();
      const releases = (result.releases || []).slice().sort((a, b) => b.version - a.version);
      if (!releases.length) { holder.append(empty('No published versions yet', 'Save your draft, then publish a version when you are ready to deliver it.', button('Return to draft', () => { packTab = 'write'; renderView(); }), true)); return; }
      const list = node('div', 'release-list');
      releases.forEach(release => {
        const info = append(node('div', 'grow'), append(node('div', 'actions'), node('h3', 'no-margin', `Version ${release.version}`), release.version === p.publishedVersion ? badge('Latest', 'good') : null), node('p', '', release.message || 'Published rule pack'), seen(release.createdAt), node('div', 'hash mono', `SHA-256 ${String(release.hash).slice(0, 16)}…`));
        const actions = append(node('div', 'release-tools'), button('View', () => showRelease(p, release), 'compact'), button('Restore to draft', () => showRestore(p.id, release), 'compact'));
        list.append(append(node('div', 'release'), node('span', 'release-circle', `v${release.version}`), info, actions));
      }); holder.append(list);
    } catch (err) { if (holder.isConnected) holder.replaceChildren(note(err.message, 'error')); }
  }
  function renderPolicy(p) {
    const aside = node('aside', 'side-stack');
    const policy = panel('Delivery policy', 'How this pack reaches its devices.');
    const form = node('form', 'panel-body');
    const mode = select([['auto', 'Automatic'], ['manual', 'Manual']], p.mode || 'auto');
    const interval = input('number', p.intervalSeconds || 60); interval.min = '15'; interval.max = '86400'; interval.step = '1'; interval.required = true;
    const paused = input('checkbox'); paused.checked = !!p.paused;
    const check = node('label', 'check-field'); append(check, paused, node('span', '', 'Pause delivery'));
    const save = button('Save policy', null, 'full'); save.type = 'submit';
    const error = node('p', 'error-inline'); error.setAttribute('role', 'alert');
    const intervalField = field('Check interval (seconds)', interval, 'Delivery interval, separate from the 15-second device heartbeat.');
    function updateMode() { interval.disabled = mode.value === 'manual'; intervalField.classList.toggle('hidden', mode.value === 'manual'); }
    mode.addEventListener('change', updateMode); updateMode();
    append(form, field('Delivery mode', mode), intervalField, check, save, error, node('div', 'divider'));
    const sync = button('↻  Sync now', async () => {
      await busy(sync, async () => {
        try { await api(`/api/packs/${encodeURIComponent(p.id)}/sync-now`, 'POST', {}); await refresh(); toast(pack(p.id)?.paused ? 'Request recorded. Delivery remains paused.' : 'Sync requested. Devices will check when they next connect.'); }
        catch (err) { showError(err); }
      });
    }, 'full');
    sync.disabled = !p.publishedVersion;
    const observed = node('p', 'policy-info'); observed.id = 'policy-observed'; append(form, sync, observed);
    form.addEventListener('submit', async event => {
      event.preventDefault(); error.textContent = '';
      await busy(save, async () => {
        try { await api(`/api/packs/${encodeURIComponent(p.id)}/policy`, 'PATCH', {mode: mode.value, intervalSeconds: Number(interval.value), paused: paused.checked}); await refresh(); toast('Delivery policy saved.'); }
        catch (err) { showError(err, error); }
      });
    }); policy.append(form); aside.append(policy);
    const scope = panel('Where it goes', 'Local targets stay under your control.');
    const scoped = state.devices.filter(d => !d.revoked && (d.packIds || []).includes(p.id));
    const body = node('div', 'panel-body');
    append(body, node('h3', '', `${scoped.length} scoped ${scoped.length === 1 ? 'device' : 'devices'}`), node('p', 'muted small', 'The Agent only writes to target files you explicitly bind on that device.'), button('Add a device', () => showEnrollment([p.id]), 'full'));
    scope.append(body); aside.append(scope);
    aside.append(note('Publishing installs files according to this policy. Existing chats are not automatically refreshed. App loading and behavior remain unverified.'));
    return aside;
  }

  function showNewPack() {
    const {body, footer} = openDialog('Create a rule pack');
    const form = node('form'); form.id = 'new-pack-form';
    const name = input(); name.required = true; name.maxLength = 100; name.placeholder = 'e.g. Writing & collaboration';
    const description = input(); description.maxLength = 500; description.placeholder = 'A short description of what this pack is for';
    const content = node('textarea'); content.placeholder = '# Working principles\n\nWrite your shared instructions here.'; content.spellcheck = false;
    const error = node('p', 'error-inline'); error.setAttribute('role', 'alert');
    append(form, field('Pack name', name), field('Description', description), field('Markdown draft', content, 'You can keep writing after creating the pack. Only a published version reaches devices.'), error); body.append(form);
    const create = button('Create draft', null, 'primary'); create.type = 'submit'; create.setAttribute('form', form.id);
    append(footer, button('Cancel', closeDialog), create);
    form.addEventListener('submit', async event => {
      event.preventDefault(); await busy(create, async () => {
        try { const result = await api('/api/packs', 'POST', {name: name.value.trim(), description: description.value.trim(), content: content.value}); closeDialog(); await refresh(); navigate('packs', result.id); toast('Rule pack created. It has not been published.'); }
        catch (err) { showError(err, error); }
      });
    }); name.focus();
  }
  function showPublish(id) {
    const p = pack(id); const d = drafts.get(id); if (!p || !d || d.dirty) return;
    const baseRevision = d.baseRevision;
    const {body, footer} = openDialog(`Publish version ${p.publishedVersion + 1}?`);
    const scoped = state.devices.filter(item => !item.revoked && (item.packIds || []).includes(id));
    append(body, node('p', '', 'This creates an immutable version of your saved draft. Delivery follows this pack’s current policy.'), node('div', 'selected-scope', `${p.name} · ${scoped.length} scoped ${scoped.length === 1 ? 'device' : 'devices'} · ${p.paused ? 'Paused' : p.mode === 'manual' ? 'Manual delivery' : 'Automatic delivery'}`));
    const message = input(); message.maxLength = 500; message.placeholder = 'What changed in this version?';
    body.append(field('Release note (optional)', message));
    if (!scoped.length) body.append(note('No devices are scoped yet. You can publish now and enroll a device later.'));
    if (p.paused || p.mode === 'manual') body.append(note(p.paused ? 'Delivery is paused. Publishing will not resume it.' : 'Manual delivery is enabled. Request Sync now when you want devices to apply this version.', 'warning'));
    const error = node('p', 'error-inline'); error.setAttribute('role', 'alert'); body.append(error);
    const confirm = button('Publish version', async () => {
      await busy(confirm, async () => {
        try { const release = await api(`/api/packs/${encodeURIComponent(id)}/publish`, 'POST', {baseRevision, message: message.value.trim()}); details.delete(id); closeDialog(); await refresh(); renderView(); toast(`Version ${release.version} published. Installation is tracked separately.`); }
        catch (err) {
          if (err.status === 409) { drafts.get(id).dirty = true; await refresh(); }
          showError(err, error);
        }
      });
    }, 'primary'); append(footer, button('Keep as draft', closeDialog), confirm);
  }
  async function showCompare(id) {
    const {body} = openDialog('Compare with published version', true); body.append(node('p', '', 'Loading the latest published version…'));
    try {
      const result = await getDetail(id); if (!dialog.open) return;
      const latest = (result.releases || []).find(r => r.version === result.pack.publishedVersion);
      body.replaceChildren(node('p', '', latest ? 'Read the published version alongside your current draft. Neither side is changed by this view.' : 'There is no published version yet.'));
      body.append(compareColumns(latest ? `Published · v${latest.version}` : 'No published version', latest?.content || '', 'Your draft', drafts.get(id).content));
    } catch (err) { body.replaceChildren(note(err.message, 'error')); }
  }
  function compareColumns(leftTitle, left, rightTitle, right) {
    return append(node('div', 'compare-grid'), append(node('div'), node('div', 'compare-label', leftTitle), node('pre', '', left || '(Empty)')), append(node('div'), node('div', 'compare-label', rightTitle), node('pre', '', right || '(Empty)')));
  }
  function showConflict(id) {
    const p = pack(id); const d = drafts.get(id); const {body, footer} = openDialog('Review the draft conflict', true);
    append(body, node('p', '', 'The saved draft changed after you started editing. Review both versions and choose a starting point. Continue with mine keeps your local text for another edit and explicit save; it does not publish.'), compareColumns('Latest Hub draft', p.content, 'Your unsaved draft', d.content));
    append(footer, button('Cancel', closeDialog), button('Use Hub draft', () => {
      drafts.set(id, {content: p.content || '', savedContent: p.content || '', baseRevision: p.draftRevision, dirty: false}); closeDialog(); renderView();
    }), button('Continue with mine', () => {
      d.baseRevision = p.draftRevision; d.savedContent = p.content || ''; d.dirty = d.content !== d.savedContent;
      closeDialog(); renderView();
    }, 'primary'));
  }
  function showRelease(p, release) {
    const {body, footer} = openDialog(`${p.name} · version ${release.version}`, true);
    append(body, node('p', '', `Immutable release · ${fullDate(release.createdAt)}`), node('p', 'mono small wrap', `SHA-256 ${release.hash}`));
    const preview = node('div', 'preview'); renderMarkdown(preview, release.content); body.append(preview);
    append(footer, button('Close', closeDialog), button('Restore to draft', () => showRestore(p.id, release)));
  }
  function showRestore(id, release) {
    const d = drafts.get(id); const p = pack(id); const {body, footer} = openDialog(`Restore version ${release.version} to draft?`);
    body.append(node('p', '', 'This copies the historical content into a new draft revision. Published history and installed files stay as they are until you explicitly publish again.'));
    if (d.dirty) body.append(note('You have unsaved edits in this tab. Save them first or explicitly discard them before restoring.', 'warning'));
    const error = node('p', 'error-inline'); body.append(error);
    const restore = button('Restore to draft', async () => {
      await busy(restore, async () => {
        try { const restored = await api(`/api/packs/${encodeURIComponent(id)}/restore`, 'POST', {version: release.version, baseRevision: p.draftRevision}); drafts.set(id, {content: restored.content, savedContent: restored.content, baseRevision: restored.draftRevision, dirty: false}); closeDialog(); await refresh(); packTab = 'write'; renderView(); toast(`Version ${release.version} restored to draft. It has not been published.`); }
        catch (err) { showError(err, error); }
      });
    }, 'primary'); restore.disabled = d.dirty;
    append(footer, button('Cancel', closeDialog), restore);
  }

  function renderDevices(content) {
    append(content, pageHead('Every device, accounted for.', 'Connection tells one story. The installed file tells another.', [button('Setup guide', showGuide), button('＋ Add device', showEnrollment, 'primary')]), renderStats());
    const section = panel('Enrolled devices', 'A device is online when its heartbeat was received within 60 seconds.');
    if (!state.devices.length) section.append(empty('Your first device starts here', 'Create a one-time enrollment token, then run the Agent on a device you own.', button('Add a device', showEnrollment, 'primary')));
    else {
      const table = node('table'); const tr = node('tr'); ['Device', 'Connection', 'Rule packs', 'Last heartbeat', 'Access'].forEach(label => tr.append(node('th', '', label))); table.append(append(node('thead'), tr));
      const body = node('tbody');
      state.devices.forEach(d => {
        const heading = append(node('td'), append(node('div', 'device-card-heading'), node('div', 'device-symbol', '▣'), append(node('div'), node('span', 'cell-title', d.name), node('span', 'cell-sub', `${d.platform || 'Platform not reported'} · ${String(d.id).slice(0, 10)}`))));
        const names = (d.packIds || []).map(id => pack(id)?.name || id).join(', ') || 'No packs';
        const status = d.revoked ? badge('Revoked') : fresh(d) ? badge('Online', 'good') : badge(ageSeconds(d.lastSeen) === Infinity ? 'Awaiting first poll' : 'Stale / offline', 'warning');
        const revoke = button('Revoke access', () => showRevoke(d), 'compact danger'); revoke.disabled = d.revoked;
        body.append(append(node('tr'), heading, append(node('td'), status), node('td', 'wrap', names), append(node('td'), seen(d.lastSeen)), append(node('td'), revoke)));
      }); table.append(body); section.append(append(node('div', 'table-wrap'), table));
    }
    append(content, section, append(node('div', 'list-space'), coveragePanel()));
  }
  function showEnrollment(preselected) {
    const selected = Array.isArray(preselected) ? preselected : [];
    const {body, footer} = openDialog('Add a device');
    append(body, node('p', '', 'Choose which rule packs this device can receive. The enrollment token expires quickly and can be used once. Local file targets are chosen on the device.'));
    if (!state.packs.length) {
      append(body, note('Create a rule pack first so you can choose what this device is allowed to receive.'));
      append(footer, button('Cancel', closeDialog), button('Create a rule pack', showNewPack, 'primary')); return;
    }
    const choices = node('div', 'scope-list'); const inputs = [];
    state.packs.forEach(p => { const check = input('checkbox'); check.value = p.id; check.checked = selected.includes(p.id); inputs.push(check); choices.append(append(node('label', 'check-field'), check, node('span', '', p.name))); });
    append(body, node('p', 'field-label', 'Allowed rule packs'), choices);
    const expires = select([['15', '15 minutes'], ['30', '30 minutes'], ['60', '60 minutes']], '15'); body.append(field('Token expires in', expires));
    const error = node('p', 'error-inline'); error.setAttribute('role', 'alert'); body.append(error);
    const generate = button('Create enrollment token', async () => {
      const ids = inputs.filter(el => el.checked).map(el => el.value);
      if (!ids.length) { error.textContent = 'Select at least one rule pack.'; return; }
      await busy(generate, async () => {
        try {
          const result = await api('/api/enrollment', 'POST', {packIds: ids, expiresMinutes: Number(expires.value)});
          body.replaceChildren(); footer.replaceChildren();
          append(body, badge('Single use · shown once', 'warning'), node('h3', 'mt', 'Save this token privately.'), node('p', '', `Expires ${fullDate(result.expiresAt)}. Save it to a private file on your device, then enroll the Agent. Do not paste it into a chat or issue.`), node('div', 'token-box mono', result.token));
          const copy = button('Copy token', async () => {
            try { await navigator.clipboard.writeText(result.token); toast('Enrollment token copied. Clear your clipboard after enrollment.'); }
            catch (_) { toast('Clipboard access is unavailable. Select and copy the token above.', true); }
          }, 'compact'); body.append(copy);
          const hub = location.origin;
          body.append(node('pre', 'command', `rulesend agent enroll --data ./rulesend-agent \\\n  --hub ${hub} --name "My device" \\\n  --token-file /private/path/enrollment-token`));
          append(body, note('The Agent stores its own device credential after enrolling. Closing this dialog removes the displayed enrollment token.'));
          append(footer, button('Close', closeDialog), button('View binding guide', showGuide, 'primary'));
          await refresh();
        } catch (err) { showError(err, error); }
      });
    }, 'primary'); append(footer, button('Cancel', closeDialog), generate);
  }
  function showRevoke(d) {
    const {body, footer} = openDialog(`Revoke ${d.name}?`);
    append(body, node('p', '', 'This device will no longer be able to poll the Hub or report file checks. Existing local files are not removed. Enroll again with a new token if you want to reconnect it.'), node('div', 'selected-scope', `${d.name} · ${d.platform || 'Unknown platform'} · ${String(d.id).slice(0, 12)}`));
    const error = node('p', 'error-inline'); body.append(error);
    const revoke = button('Revoke device access', async () => {
      await busy(revoke, async () => {
        try { await api(`/api/devices/${encodeURIComponent(d.id)}/revoke`, 'POST', {}); closeDialog(); await refresh(); toast(`${d.name} access revoked.`); }
        catch (err) { showError(err, error); }
      });
    }, 'danger'); append(footer, button('Cancel', closeDialog), revoke);
  }
  function renderActivity(content) {
    content.append(pageHead('A clear trail of what changed.', 'Published versions, device updates, and reported outcomes.'));
    const section = panel('Recent activity', 'Recorded by this Hub. New entries appear on the next refresh.');
    if (!state.events.length) section.append(empty('A fresh start', 'Create a rule pack or enroll a device. Recorded activity will appear here.', null, true));
    else {
      const list = node('div', 'activity-list');
      state.events.slice().sort((a, b) => Number(b.id) - Number(a.id)).forEach(event => list.append(append(node('div', 'activity-item'), node('span', 'activity-mark', '↗'), append(node('div', 'grow'), node('div', 'activity-type', event.kind || 'Activity'), node('p', '', event.message), seen(event.createdAt), node('span', 'muted small', event.actor ? ` · ${event.actor}` : '')))));
      section.append(list);
    } content.append(section);
  }
  function showGuide() {
    const {body, footer} = openDialog('Connect a device, deliberately.', true);
    body.append(node('p', '', 'Run these commands on the device where the target files live. Replace the placeholders with your own choices. The Hub cannot choose local paths for you.'));
    const steps = [
      ['1. Enroll with a one-time token', 'Create a token with Add device and save it to a private local file.', `rulesend agent enroll --data ./rulesend-agent \\\n  --hub ${location.origin} --name "My device" \\\n  --token-file /private/path/enrollment-token`],
      ['2. Bind an explicit target', 'Use codex, claude, or markdown as the adapter. The parent directory must already exist. One target file contains one RuleSend pack.', `rulesend agent bind --data ./rulesend-agent \\\n  --pack PACK_ID --adapter codex \\\n  --path /absolute/path/AGENTS.md --label "Project · Codex"`],
      ['3. Apply or keep the Agent running', 'Once explicitly applies published versions and still respects pause. Run follows your policy and sends heartbeats. Stop Run before using another Agent command.', 'rulesend agent once --data ./rulesend-agent\nrulesend agent run --data ./rulesend-agent\nrulesend agent status --data ./rulesend-agent']
    ];
    steps.forEach(([title, text, command]) => body.append(append(node('section', 'guide-step'), node('h3', '', title), node('p', '', text), node('pre', 'command', command))));
    if (selectedPack && pack(selectedPack)) body.append(append(node('div', 'notice mt'), node('strong', '', 'Selected pack ID: '), node('code', '', selectedPack)));
    body.append(note('Existing text outside RuleSend’s managed block is preserved. Local changes inside the block stop automatic replacement. For a remote Hub, use HTTPS. File installation does not prove an AI has loaded or followed the rules.'));
    footer.append(button('Got it', closeDialog, 'primary'));
  }

  /* Small, intentionally conservative Markdown renderer. All source text becomes DOM text;
     HTML, images, links, and attributes are never copied from the document. */
  function inlineMarkdown(parent, text) {
    const pattern = /(\*\*([^*\n]+)\*\*|`([^`\n]+)`|\*([^*\n]+)\*)/g;
    let at = 0; let match;
    while ((match = pattern.exec(text))) {
      parent.append(document.createTextNode(text.slice(at, match.index)));
      parent.append(node(match[2] ? 'strong' : match[3] ? 'code' : 'em', '', match[2] || match[3] || match[4]));
      at = pattern.lastIndex;
    }
    parent.append(document.createTextNode(text.slice(at)));
  }
  function renderMarkdown(parent, source) {
    parent.replaceChildren();
    if (!source.trim()) { parent.append(node('p', 'muted', 'Your Markdown preview will appear here.')); return; }
    const lines = source.replace(/\r\n/g, '\n').split('\n'); let paragraph = []; let list = null; let code = null;
    function flushParagraph() { if (paragraph.length) { const p = node('p'); inlineMarkdown(p, paragraph.join('\n')); parent.append(p); paragraph = []; } }
    function resetList() { list = null; }
    lines.forEach(line => {
      if (/^\s*```/.test(line)) {
        flushParagraph(); resetList();
        if (code !== null) { parent.append(append(node('pre'), node('code', '', code.join('\n')))); code = null; } else code = [];
        return;
      }
      if (code !== null) { code.push(line); return; }
      if (!line.trim()) { flushParagraph(); resetList(); return; }
      const heading = /^(#{1,6})\s+(.*)$/.exec(line);
      if (heading) { flushParagraph(); resetList(); const h = node(`h${heading[1].length}`); inlineMarkdown(h, heading[2]); parent.append(h); return; }
      if (/^\s*(---+|\*\*\*+)\s*$/.test(line)) { flushParagraph(); resetList(); parent.append(node('hr')); return; }
      const item = /^\s*([-*+] |\d+\. )(.*)$/.exec(line);
      if (item) {
        flushParagraph(); const type = /^\d/.test(item[1]) ? 'ol' : 'ul';
        if (!list || list.tagName.toLowerCase() !== type) { list = node(type); parent.append(list); }
        const li = node('li'); inlineMarkdown(li, item[2]); list.append(li); return;
      }
      const quote = /^>\s?(.*)$/.exec(line);
      if (quote) { flushParagraph(); resetList(); const q = node('blockquote'); inlineMarkdown(q, quote[1]); parent.append(q); return; }
      resetList(); paragraph.push(line);
    });
    flushParagraph(); if (code !== null) parent.append(append(node('pre'), node('code', '', code.join('\n'))));
  }
  window.addEventListener('beforeunload', event => { if ([...drafts.values()].some(d => d.dirty)) { event.preventDefault(); event.returnValue = ''; } });
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible' && authenticated) refresh(); });
  app.append(append(node('div', 'loading-screen'), append(node('div', 'loading'), node('div', 'spinner'), node('p', '', 'Connecting to your RuleSend Hub…'))));
  refresh(true);
})();
