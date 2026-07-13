// ttl web client — minimal vanilla JS + htmx for partial updates.
// No build step. All API calls go to /api/v1 with credentials.

(function () {
  const $ = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  // Expose window.ttl FIRST. Function declarations below are hoisted
  // to the top of this IIFE, so referencing them here works.
  window.ttl = { boot, bootAuth };

  let installPrompt;
  window.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault();
    installPrompt = event;
    $$('[data-install-app]').forEach((button) => { button.hidden = false; });
  });
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-install-app]');
    if (!button || !installPrompt) return;
    await installPrompt.prompt();
    await installPrompt.userChoice;
    installPrompt = null;
    $$('[data-install-app]').forEach((item) => { item.hidden = true; });
  });
  window.addEventListener('appinstalled', () => {
    installPrompt = null;
    $$('[data-install-app]').forEach((button) => { button.hidden = true; });
  });
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js'));
  }

  async function api(method, path, body) {
    const opt = {
      method,
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
    };
    if (body !== undefined) opt.body = JSON.stringify(body);
    const r = await fetch(path, opt);
    if (r.status === 204) return null;
    const j = await r.json().catch(() => ({}));
    if (!r.ok) {
      const msg = (j.error && j.error.message) || ('http ' + r.status);
      throw new Error(msg);
    }
    return j;
  }

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  function fmtDue(due) {
    if (!due) return '';
    const d = new Date(due);
    const today = new Date();
    const t = new Date(today.getFullYear(), today.getMonth(), today.getDate());
    const diffDays = Math.round((d - t) / 86400000);
    if (diffDays < 0) return '<span class="due-overdue">' + d.toISOString().slice(0, 10) + ' (overdue)</span>';
    if (diffDays === 0) return '<span class="due-today">today</span>';
    if (diffDays === 1) return 'tomorrow';
    return d.toISOString().slice(5, 10);
  }

  function fmtCompleted(completedAt) {
    if (!completedAt) return '';
    return 'completed at ' + new Date(completedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  function localDateValue(d) {
    const date = d || new Date();
    const offset = date.getTimezoneOffset() * 60000;
    return new Date(date.getTime() - offset).toISOString().slice(0, 10);
  }

  function isToday(value) {
    return value && localDateValue(new Date(value)) === localDateValue();
  }

  function priLabel(p) {
    if (p === 3) return '<span class="pri-high">!!</span>';
    if (p === 2) return '<span class="pri-med">!</span>';
    if (p === 1) return '<span class="pri-low">-</span>';
    return '';
  }

  function renderTask(t, context) {
    const tags = (t.tags || []).map((n) => '<span class="tag">' + escapeHTML(n) + '</span>').join('');
    const timing = context === 'completed' ? fmtCompleted(t.completed_at) : fmtDue(t.due_at);
    const cls = t.status === 'done' ? 'title done' : 'title';
    // Layout: [checkbox] [pri] [title + meta] [del]
    // Title and meta share a column that wraps on narrow screens.
    const deleted = !!t.deleted_at;
    const actions = deleted
      ? '<button class="secondary small" data-act="restore">restore</button><button class="del" data-act="purge" title="permanently delete">purge</button>'
      : '<button class="del" data-act="del" title="move to trash">x</button>';
    return `
      <li data-id="${t.id}">
        ${deleted ? '' : `<input type="checkbox" ${t.status === 'done' ? 'checked' : ''} data-act="toggle">`}
        <span class="pri">${priLabel(t.priority)}</span>
        <div class="body">
          <button class="task-title-button ${cls}" data-act="edit">${escapeHTML(t.title)}</button>
          <span class="meta">${timing} ${tags}</span>
        </div>
        ${actions}
      </li>`;
  }

  async function renderList(targetEl, params) {
    try {
      const q = new URLSearchParams(params || {}).toString();
      const data = await api('GET', '/api/v1/tasks?' + q);
      if (!data.tasks || data.tasks.length === 0) {
        targetEl.innerHTML = '<div class="empty">No tasks here yet.</div>';
        return;
      }
      targetEl.innerHTML = '<ul class="tasklist">' +
        data.tasks.map(renderTask).join('') + '</ul>';
      wireList(targetEl);
    } catch (e) {
      targetEl.innerHTML = '<div class="empty error">' + escapeHTML(e.message) + '</div>';
    }
  }

  // Render the active timer + today's work log. Returns the HTML.
  async function fetchWorklog() {
    try {
      const r = await api('GET', '/api/v1/worklog/today');
      return r || { summary: { per_task: [], total_ms: 0, day: new Date().toISOString() }, active: null };
    } catch { return { summary: { per_task: [], total_ms: 0 }, active: null }; }
  }

  async function fetchProductivityTrend(days) {
    try {
      const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
      return await api('GET', '/api/v1/analytics/productivity?days=' + days + '&tz=' + encodeURIComponent(tz));
    } catch { return { days: [] }; }
  }

  function fmtDur(ms) {
    const s = Math.floor(ms / 1000);
    const m = Math.floor(s / 60);
    const h = Math.floor(m / 60);
    if (h) return h + 'h ' + (m % 60) + 'm';
    if (m) return m + 'm ' + (s % 60) + 's';
    return s + 's';
  }

  function renderWorklogHTML(w, openTasks) {
    const active = w.active;
    const sum = w.summary || {};
    let activeHTML = '';
    if (active) {
      const elapsed = Date.now() - new Date(active.started_at).getTime();
      activeHTML = '<div class="active-timer" id="active-timer" data-started="' + active.started_at + '">' +
        '<span class="dot"></span>' +
        '<b>' + (active.kind === 'pomodoro' ? 'pomodoro' : 'tracking') + ':</b> ' +
        escapeHTML(active.task_title || '(no task)') +
        '  <span class="elapsed">' + fmtDur(elapsed) + '</span>' +
        ' <button class="secondary small" data-act="stop">stop</button>' +
        '</div>';
    }
    const perTask = sum.per_task || [];
    let perTaskHTML = '';
    if (perTask.length > 0) {
      perTaskHTML = '<table class="worklog"><tr><th>TIME</th><th>ENTRIES</th><th>TASK</th></tr>' +
        perTask.map((p) =>
          '<tr><td>' + fmtDur(p.total_ms) + '</td><td>' + p.count + '</td><td>' + escapeHTML(p.task_title) + '</td></tr>'
        ).join('') + '</table>';
    } else {
      perTaskHTML = '<div class="muted">No completed entries today.</div>';
    }
    const total = '<div class="muted">Total tracked: <b>' + fmtDur(sum.total_ms || 0) + '</b></div>';
    let focusHTML = '';
    if (!active) {
      focusHTML = '<form class="focus-controls" id="pomodoro-form">' +
        '<b>Start a focus session</b>' +
        '<select name="task_id" aria-label="Task"><option value="">General focus</option>' +
        (openTasks || []).map((t) => '<option value="' + t.id + '">' + escapeHTML(t.title) + '</option>').join('') +
        '</select>' +
        '<select name="minutes" aria-label="Duration"><option value="25">25 min</option><option value="50">50 min</option></select>' +
        '<button type="submit">Start Pomodoro</button></form>';
    }
    return activeHTML + focusHTML + total + perTaskHTML;
  }

  function wireWorklog(root) {
    const stopBtn = root.querySelector('[data-act="stop"]');
    if (stopBtn) {
      stopBtn.addEventListener('click', async () => {
        if (!confirm('Stop the running timer?')) return;
        try {
          await api('POST', '/api/v1/timer/stop', {});
          await boot();
        } catch (e) { alert(e.message); }
      });
    }
    const pomodoro = root.querySelector('#pomodoro-form');
    if (pomodoro) {
      pomodoro.addEventListener('submit', async (e) => {
        e.preventDefault();
        const taskID = e.target.elements.task_id.value;
        const minutes = Number(e.target.elements.minutes.value);
        try {
          await api('POST', '/api/v1/timer/start', { task_id: taskID, kind: 'pomodoro', minutes });
          await boot();
        } catch (err) { alert(err.message); }
      });
    }
    // Tick the elapsed counter every second.
    const at = root.querySelector('#active-timer');
    if (at) {
      const started = new Date(at.dataset.started).getTime();
      const span = at.querySelector('.elapsed');
      setInterval(() => {
        if (!span) return;
        span.textContent = fmtDur(Date.now() - started);
      }, 1000);
    }
  }

  function wireList(root) {
    $$('[data-act="edit"]', root).forEach((b) => b.addEventListener('click', (e) => openTaskEditor(e.target.closest('li').dataset.id)));
    $$('input[data-act="toggle"]', root).forEach((cb) => {
      cb.addEventListener('change', async (e) => {
        const li = e.target.closest('li');
        const id = li.dataset.id;
        try {
          await api('POST', '/api/v1/tasks/' + id + '/complete');
          await boot();
        } catch (err) { alert(err.message); }
      });
    });
    $$('button[data-act="del"]', root).forEach((b) => {
      b.addEventListener('click', async (e) => {
        const li = e.target.closest('li');
        const id = li.dataset.id;
        if (!confirm('Move this task to trash?')) return;
        try {
          await api('DELETE', '/api/v1/tasks/' + id);
          await boot();
        } catch (err) { alert(err.message); }
      });
    });
    $$('button[data-act="restore"]', root).forEach((b) => b.addEventListener('click', async (e) => {
      try { await api('POST', '/api/v1/tasks/' + e.target.closest('li').dataset.id + '/restore'); await boot(); }
      catch (err) { alert(err.message); }
    }));
    $$('button[data-act="purge"]', root).forEach((b) => b.addEventListener('click', async (e) => {
      if (!confirm('Permanently delete this task? This cannot be undone.')) return;
      try { await api('DELETE', '/api/v1/tasks/' + e.target.closest('li').dataset.id + '/purge'); await boot(); }
      catch (err) { alert(err.message); }
    }));
  }

  async function openTaskEditor(id) {
    const dialog = $('#task-dialog');
    const form = $('#task-edit-form');
    try {
      const t = await api('GET', '/api/v1/tasks/' + id);
      form.elements.id.value = t.id;
      form.elements.title.value = t.title || '';
      form.elements.notes.value = t.notes || '';
      form.elements.priority.value = String(t.priority || 0);
      form.elements.tags.value = (t.tags || []).join(',');
      const repeat = recurrenceFormValue(t.recurrence_rrule || '');
      form.elements.repeat.value = repeat.preset;
      form.elements.repeat_custom.value = repeat.custom;
      updateRepeatFields(form);
      form.elements.due.value = t.due_at ? new Date(t.due_at).toISOString().slice(0, 16) : '';
      $('#subtask-list').innerHTML = (t.subtasks || []).map(renderTask).join('');
      wireList($('#subtask-list'));
      if (!dialog.open) dialog.showModal();
    } catch (err) { alert(err.message); }
  }

  function wireTaskEditor() {
    const dialog = $('#task-dialog');
    const form = $('#task-edit-form');
    $$('[data-act="cancel-edit"]', dialog).forEach((b) => b.addEventListener('click', () => dialog.close()));
    form.elements.repeat.addEventListener('change', () => updateRepeatFields(form));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const due = form.elements.due.value;
      const body = {
        title: form.elements.title.value.trim(), notes: form.elements.notes.value,
        priority: Number(form.elements.priority.value), due_at: due ? new Date(due).getTime() : null,
        tags: form.elements.tags.value.split(',').map((s) => s.trim()).filter(Boolean),
        recurrence_rrule: recurrenceValue(form),
      };
      try { await api('PATCH', '/api/v1/tasks/' + form.elements.id.value, body); dialog.close(); await boot(); }
      catch (err) { alert(err.message); }
    });
    $('#subtask-form', dialog).addEventListener('submit', async (e) => {
      e.preventDefault(); const title = e.target.elements.title.value.trim(); if (!title) return;
      try {
		const created = await api('POST', '/api/v1/tasks', { title, priority: 0, parent_id: form.elements.id.value });
		e.target.reset();
		const list = $('#subtask-list');
		list.insertAdjacentHTML('beforeend', renderTask(created));
		wireList(list.lastElementChild);
	  }
      catch (err) { alert(err.message); }
    });
    $('#reminder-form', dialog).addEventListener('submit', async (e) => {
      e.preventDefault(); const at = e.target.elements.at.value;
      try { await api('POST', '/api/v1/reminders', { task_id: form.elements.id.value, fire_at: new Date(at).getTime() }); e.target.reset(); alert('Reminder scheduled'); }
      catch (err) { alert(err.message); }
    });
  }

  function recurrenceFormValue(rule) {
    const presets = {
      'FREQ=DAILY': 'daily',
      'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR': 'weekdays',
      'FREQ=WEEKLY': 'weekly',
      'FREQ=MONTHLY': 'monthly',
      'FREQ=YEARLY': 'yearly',
    };
    return presets[rule] ? { preset: presets[rule], custom: '' } :
      (rule ? { preset: 'custom', custom: rule } : { preset: '', custom: '' });
  }

  function updateRepeatFields(form) {
    $('.repeat-custom', form).hidden = form.elements.repeat.value !== 'custom';
  }

  function recurrenceValue(form) {
    const preset = form.elements.repeat.value;
    if (preset === 'custom') return form.elements.repeat_custom.value.trim() || null;
    return preset || null;
  }

  async function composer(root, params) {
    const form = $('form.composer', root);
    if (!form) return;
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const title = form.elements['title'].value.trim();
      if (!title) return;
      const body = { title, priority: 0 };
	  if (params && params.project_id) body.project_id = params.project_id;
      const tagsRaw = form.elements['tags'] ? form.elements['tags'].value.trim() : '';
      if (tagsRaw) body.tags = tagsRaw.split(',').map((s) => s.trim()).filter(Boolean);
      const dueRaw = form.elements['due'] ? form.elements['due'].value.trim() : '';
      if (dueRaw) {
        const t = new Date(dueRaw);
        if (!isNaN(t)) body.due_at = t.getTime();
      }
      try {
        await api('POST', '/api/v1/tasks', body);
        form.reset();
        await boot();
      } catch (err) { alert(err.message); }
    });
  }

  async function me() {
    try { return await api('GET', '/api/v1/me'); }
    catch { return null; }
  }

  async function boot() {
    const u = await me();
    if (!u) {
      window.location.href = '/login';
      return;
    }
    const email = $('#user-email');
    if (email) email.textContent = u.email;

    const path = window.location.pathname;
    const main = $('#main');
    let title, params = { status: 'open', limit: '500' };
    if (path.startsWith('/today')) {
      title = 'Today';
      params = { view: 'today', limit: '500' };
      try {
        const [data, doneData, wl, trend] = await Promise.all([
          api('GET', '/api/v1/tasks?view=today&limit=500'),
          api('GET', '/api/v1/tasks?view=done&limit=500'),
          fetchWorklog(),
          fetchProductivityTrend(14),
        ]);
        const filtered = data.tasks || [];
        const completedToday = (doneData.tasks || []).filter((t) => isToday(t.completed_at));
        main.innerHTML = composerHTML(title) +
          '<section class="analytics" id="analytics"></section>' +
          '<section class="trend-panel" id="trend"></section>' +
          '<section class="worklog" id="worklog"></section>' +
          '<section><h3>Due today</h3><div id="taskhost"></div></section>' +
          '<section class="completed-today"><h3>Completed today</h3><div id="completed-host"></div></section>';
        composer(main, params);
        $('#analytics', main).innerHTML = renderAnalyticsHTML(filtered, completedToday, wl);
        $('#trend', main).innerHTML = renderTrendHTML(trend);
        const wlh = $('#worklog', main);
        wlh.innerHTML = renderWorklogHTML(wl, filtered);
        wireWorklog(wlh);
        const host = $('#taskhost', main);
        if (filtered.length === 0) {
          host.innerHTML = '<div class="empty">Nothing due today.</div>';
        } else {
          host.innerHTML = '<ul class="tasklist">' + filtered.map(renderTask).join('') + '</ul>';
          wireList(host);
        }
        const completedHost = $('#completed-host', main);
        if (completedToday.length === 0) {
          completedHost.innerHTML = '<div class="empty">No tasks completed yet today.</div>';
        } else {
          completedHost.innerHTML = '<ul class="tasklist">' + completedToday.map((t) => renderTask(t, 'completed')).join('') + '</ul>';
          wireList(completedHost);
        }
        highlightNav('today');
        return;
      } catch (e) {
        main.innerHTML = '<div class="empty error">' + escapeHTML(e.message) + '</div>';
        return;
      }
    } else if (path.startsWith('/inbox')) {
      title = 'Inbox';
      params = { view: 'inbox', limit: '500' };
      await renderMain(main, title, params);
      highlightNav('inbox');
    } else if (path.startsWith('/upcoming')) {
      await renderMain(main, 'Upcoming', { view: 'upcoming', limit: '500' }); highlightNav('upcoming');
    } else if (path.startsWith('/next')) {
      await renderMain(main, 'Next', { view: 'next', limit: '50' }); highlightNav('next');
    } else if (path.startsWith('/done')) {
      await renderMain(main, 'Done', { view: 'done', limit: '500' }); highlightNav('done');
    } else if (path.startsWith('/trash')) {
      await renderMain(main, 'Trash', { view: 'trash', limit: '500' }); highlightNav('trash');
    } else if (path.startsWith('/projects/')) {
	  await renderProjectDetail(main, decodeURIComponent(path.slice('/projects/'.length)));
	  highlightNav('projects');
    } else if (path.startsWith('/projects')) {
	  await renderProjects(main);
      highlightNav('projects');
    } else if (path.startsWith('/settings')) {
      await renderSettings(main);
      highlightNav('settings');
    } else {
      window.location.href = '/today';
    }
  }

  function renderAnalyticsHTML(openTasks, completedTasks, worklog) {
    const sum = worklog.summary || {};
    const sessions = (sum.per_task || []).reduce((n, item) => n + Number(item.count || 0), 0);
    const cards = [
      ['Completed', completedTasks.length],
      ['Remaining', openTasks.length],
      ['Focus time', fmtDur(sum.total_ms || 0)],
      ['Sessions', sessions],
    ];
    return cards.map((card) => '<div class="metric"><span>' + card[0] + '</span><b>' + card[1] + '</b></div>').join('');
  }

  function renderTrendHTML(trend) {
    const days = (trend && trend.days) || [];
    if (days.length === 0) return '<div class="empty">Historical trends are not available yet.</div>';
    const maxCompleted = Math.max(1, ...days.map((d) => d.completed || 0));
    const maxFocus = Math.max(1, ...days.map((d) => d.focus_ms || 0));
    return '<div class="trend-heading"><h3>14-day trend</h3><div class="trend-legend"><span class="tasks-key">tasks</span><span class="focus-key">focus</span></div></div>' +
      '<div class="trend-chart">' + days.map((d) => {
        const taskHeight = d.completed ? Math.max(4, Math.round((d.completed / maxCompleted) * 100)) : 0;
        const focusHeight = d.focus_ms ? Math.max(4, Math.round((d.focus_ms / maxFocus) * 100)) : 0;
        const label = d.day.slice(5);
        const detail = d.completed + ' completed, ' + fmtDur(d.focus_ms || 0) + ' focus, ' + d.sessions + ' sessions';
        return '<div class="trend-day" title="' + escapeHTML(d.day + ': ' + detail) + '" aria-label="' + escapeHTML(d.day + ': ' + detail) + '">' +
          '<div class="trend-bars"><span class="trend-bar tasks" style="height:' + taskHeight + '%"></span>' +
          '<span class="trend-bar focus" style="height:' + focusHeight + '%"></span></div>' +
          '<small>' + label + '</small></div>';
      }).join('') + '</div>';
  }

  async function renderMain(main, title, params) {
	const canCreate = params && params.view === 'inbox';
	main.innerHTML = (canCreate ? composerHTML(title) : '<h2>' + escapeHTML(title) + '</h2>') + '<div id="taskhost"></div>';
	if (canCreate) composer(main, params);
    await renderList($('#taskhost', main), params);
  }

  function composerHTML(title) {
    return `
      <h2>${escapeHTML(title)}</h2>
      <form class="composer">
        <input name="title" placeholder="What needs doing?" autofocus>
        <input name="tags" placeholder="tag1,tag2" style="max-width:160px">
        <input name="due" type="date" value="${localDateValue()}" style="max-width:160px" aria-label="Due date">
        <button type="submit">Add</button>
      </form>`;
  }

  async function renderProjects(main) {
    try {
      const data = await api('GET', '/api/v1/projects');
      const ps = data.projects || [];
      main.innerHTML = `
        <h2>Projects</h2>
        <form class="composer" id="proj-form">
          <input name="name" placeholder="Project name" required>
          <button type="submit">Create</button>
        </form>
        <ul class="tasklist">
          ${ps.map((p) => `<li><span class="title"><a href="/projects/${encodeURIComponent(p.id)}">${escapeHTML(p.name)}</a></span><span class="meta">${escapeHTML(p.color || '')}</span></li>`).join('')}
        </ul>`;
      $('#proj-form', main).addEventListener('submit', async (e) => {
        e.preventDefault();
        const name = e.target.elements['name'].value.trim();
        if (!name) return;
        await api('POST', '/api/v1/projects', { name });
        await boot();
      });
    } catch (err) {
      main.innerHTML = '<div class="empty error">' + escapeHTML(err.message) + '</div>';
    }
  }

  async function renderProjectDetail(main, id) {
	try {
	  const data = await api('GET', '/api/v1/projects');
	  const project = (data.projects || []).find((p) => p.id === id);
	  if (!project) throw new Error('Project not found');
	  main.innerHTML = `
		<div class="page-heading"><h2>${escapeHTML(project.name)}</h2><div>
		  <button class="secondary small" id="project-rename">Rename</button>
		  <button class="secondary small" id="project-archive">Archive</button>
		</div></div>
		${composerHTML('Add task')}
		<div id="taskhost"></div>`;
	  const params = { project_id: id, status: 'open', limit: '500' };
	  composer(main, params);
	  await renderList($('#taskhost', main), params);
	  $('#project-rename', main).addEventListener('click', async () => {
		const name = prompt('Project name', project.name);
		if (!name || !name.trim()) return;
		try { await api('PATCH', '/api/v1/projects/' + id, { name: name.trim(), color: project.color || '#888888' }); await boot(); }
		catch (err) { alert(err.message); }
	  });
	  $('#project-archive', main).addEventListener('click', async () => {
		if (!confirm('Archive this project? Its tasks will remain available.')) return;
		try { await api('POST', '/api/v1/projects/' + id + '/archive'); window.location.href = '/projects'; }
		catch (err) { alert(err.message); }
	  });
	} catch (err) {
	  main.innerHTML = '<div class="empty error">' + escapeHTML(err.message) + '</div>';
	}
  }

  async function renderSettings(main) {
    try {
      const [keyData, memberData] = await Promise.all([
        api('GET', '/api/v1/api-keys').catch(() => ({ api_keys: [] })),
        api('GET', '/api/v1/members').catch(() => ({ members: [] })),
      ]);
      main.innerHTML = `
        <h2>Settings</h2>
        <h3>API keys</h3>
        <p class="muted">Issue a least-privilege key for an agent or script.</p>
        <form id="key-form">
          <label>Name <input name="name" value="cli" required></label>
          <label>Scopes <input name="scopes" value="tasks:read,tasks:write" required></label>
          <button type="submit">Issue API key</button>
        </form>
        <pre id="key-result" class="ok"></pre>
		<ul class="tasklist" id="key-list">${(keyData.api_keys || []).map(renderKey).join('')}</ul>
        <h3>Members</h3>
        <ul class="tasklist">${(memberData.members || []).map((m) => `<li><span class="title">${escapeHTML(m.email)}</span><span class="meta">${escapeHTML(m.role)}</span></li>`).join('')}</ul>
        <form id="invite-form"><label>Role <select name="role"><option>member</option><option>admin</option></select></label><button type="submit">Create 7-day invite</button></form>
        <pre id="invite-result" class="ok"></pre>
        <p><button class="secondary" id="logout-btn">Log out</button></p>`;
      $('#key-form', main).addEventListener('submit', async (e) => {
        e.preventDefault();
        const name = e.target.elements['name'].value.trim();
        const scopes = e.target.elements['scopes'].value.split(',').map((s) => s.trim()).filter(Boolean);
        try {
          const r = await api('POST', '/api/v1/api-keys', { name, scopes });
          $('#key-result', main).textContent = 'API key (save this, shown once):\n' + r.key;
		  const list = $('#key-list', main);
		  list.insertAdjacentHTML('afterbegin', renderKey(r.api_key));
		  wireKeyRevocation(list.firstElementChild);
        } catch (err) { alert(err.message); }
      });
	  wireKeyRevocation(main);

	  function wireKeyRevocation(root) {
		$$('[data-act="revoke-key"]', root).forEach((b) => b.addEventListener('click', async (e) => {
        if (!confirm('Revoke this API key?')) return;
        await api('DELETE', '/api/v1/api-keys/' + e.target.closest('li').dataset.keyId); await boot();
		}));
	  }
      $('#invite-form', main).addEventListener('submit', async (e) => {
        e.preventDefault();
        try { const r = await api('POST', '/api/v1/invites', { role: e.target.elements['role'].value }); $('#invite-result', main).textContent = 'Invite token (shown once):\n' + r.token; }
        catch (err) { alert(err.message); }
      });
      $('#logout-btn', main).addEventListener('click', async () => {
        await api('POST', '/api/v1/auth/logout');
        window.location.href = '/login';
      });
    } catch (err) {
      main.innerHTML = '<div class="empty error">' + escapeHTML(err.message) + '</div>';
    }
  }

  function renderKey(k) {
	return `<li data-key-id="${k.id}"><span class="title">${escapeHTML(k.name)}</span><span class="meta">${escapeHTML((k.scopes || []).join(', '))}</span><button class="del" data-act="revoke-key">revoke</button></li>`;
  }

  function highlightNav(active) {
    $$('.topbar nav a').forEach((a) => {
      if (a.getAttribute('href').startsWith('/' + active)) a.classList.add('active');
    });
  }

  if ($('#task-dialog')) wireTaskEditor();

  // ---- Auth (login.html) ----
  function bootAuth() {
    const showLogin = $('#show-login');
    const showSignup = $('#show-signup');
    const loginForm = $('#login-form');
    const signupForm = $('#signup-form');
    if (!showLogin) return;
    showLogin.addEventListener('click', () => {
      loginForm.hidden = false; signupForm.hidden = true;
      showLogin.classList.add('secondary'); showSignup.classList.remove('secondary');
    });
    showSignup.addEventListener('click', () => {
      signupForm.hidden = false; loginForm.hidden = true;
      showSignup.classList.add('secondary'); showLogin.classList.remove('secondary');
    });
    showLogin.click();

    loginForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const email = loginForm.elements['email'].value.trim();
      const password = loginForm.elements['password'].value;
      try {
        await api('POST', '/api/v1/auth/login', { email, password });
        window.location.href = '/today';
      } catch (err) { $('#login-error').textContent = err.message; }
    });
    signupForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const tenant_name = signupForm.elements['tenant_name'].value.trim();
      const email = signupForm.elements['email'].value.trim();
      const password = signupForm.elements['password'].value;
      const invite_token = signupForm.elements['invite_token'].value.trim();
      try {
        await api('POST', '/api/v1/auth/signup', { tenant_name, email, password, invite_token });
        window.location.href = '/today';
      } catch (err) { $('#signup-error').textContent = err.message; }
    });
  }
})();
