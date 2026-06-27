// ttl web client — minimal vanilla JS + htmx for partial updates.
// No build step. All API calls go to /api/v1 with credentials.

(function () {
  const $ = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  // Expose window.ttl FIRST. Function declarations below are hoisted
  // to the top of this IIFE, so referencing them here works.
  window.ttl = { boot, bootAuth };

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

  function priLabel(p) {
    if (p === 3) return '<span class="pri-high">!!</span>';
    if (p === 2) return '<span class="pri-med">!</span>';
    if (p === 1) return '<span class="pri-low">-</span>';
    return '';
  }

  function renderTask(t) {
    const tags = (t.tags || []).map((n) => '<span class="tag">' + escapeHTML(n) + '</span>').join('');
    const cls = t.status === 'done' ? 'title done' : 'title';
    // Layout: [checkbox] [pri] [title + meta] [del]
    // Title and meta share a column that wraps on narrow screens.
    return `
      <li data-id="${t.id}">
        <input type="checkbox" ${t.status === 'done' ? 'checked' : ''} data-act="toggle">
        <span class="pri">${priLabel(t.priority)}</span>
        <div class="body">
          <span class="${cls}">${escapeHTML(t.title)}</span>
          <span class="meta">${fmtDue(t.due_at)} ${tags}</span>
        </div>
        <button class="del" data-act="del" title="delete">x</button>
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

  function fmtDur(ms) {
    const s = Math.floor(ms / 1000);
    const m = Math.floor(s / 60);
    const h = Math.floor(m / 60);
    if (h) return h + 'h ' + (m % 60) + 'm';
    if (m) return m + 'm ' + (s % 60) + 's';
    return s + 's';
  }

  function renderWorklogHTML(w) {
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
    return activeHTML + total + perTaskHTML;
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
        if (!confirm('Delete this task?')) return;
        try {
          await api('DELETE', '/api/v1/tasks/' + id);
          await boot();
        } catch (err) { alert(err.message); }
      });
    });
  }

  async function composer(root, params) {
    const form = $('form.composer', root);
    if (!form) return;
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const title = form.elements['title'].value.trim();
      if (!title) return;
      const body = { title, priority: 0 };
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
      const today = new Date();
      const end = new Date(today.getFullYear(), today.getMonth(), today.getDate(), 23, 59, 59, 999);
      params.status = 'open';
      try {
        const data = await api('GET', '/api/v1/tasks?status=open&limit=500');
        const filtered = (data.tasks || []).filter((t) => t.due_at && new Date(t.due_at) <= end);
        const wl = await fetchWorklog();
        main.innerHTML = composerHTML(title) +
          '<section class="worklog" id="worklog"></section>' +
          '<div id="taskhost"></div>';
        composer(main, params);
        const wlh = $('#worklog', main);
        wlh.innerHTML = renderWorklogHTML(wl);
        wireWorklog(wlh);
        const host = $('#taskhost', main);
        if (filtered.length === 0) {
          host.innerHTML = '<div class="empty">Nothing due today.</div>';
        } else {
          host.innerHTML = '<ul class="tasklist">' + filtered.map(renderTask).join('') + '</ul>';
          wireList(host);
        }
        highlightNav('today');
        return;
      } catch (e) {
        main.innerHTML = '<div class="empty error">' + escapeHTML(e.message) + '</div>';
        return;
      }
    } else if (path.startsWith('/inbox')) {
      title = 'Inbox';
      params.parent_id = 'root';
      await renderMain(main, title, params);
      highlightNav('inbox');
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

  async function renderMain(main, title, params) {
    main.innerHTML = composerHTML(title) + '<div id="taskhost"></div>';
    composer(main, params);
    await renderList($('#taskhost', main), params);
  }

  function composerHTML(title) {
    return `
      <h2>${escapeHTML(title)}</h2>
      <form class="composer">
        <input name="title" placeholder="What needs doing?" autofocus>
        <input name="tags" placeholder="tag1,tag2" style="max-width:160px">
        <input name="due" type="date" style="max-width:160px">
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

  async function renderSettings(main) {
    try {
      main.innerHTML = `
        <h2>Settings</h2>
        <p class="muted">Issue an API key for the CLI / MCP / scripts.</p>
        <form id="key-form">
          <label>Name <input name="name" value="cli" required></label>
          <button type="submit">Issue API key</button>
        </form>
        <pre id="key-result" class="ok"></pre>
        <p><button class="secondary" id="logout-btn">Log out</button></p>`;
      $('#key-form', main).addEventListener('submit', async (e) => {
        e.preventDefault();
        const name = e.target.elements['name'].value.trim();
        try {
          const r = await api('POST', '/api/v1/api-keys', { name });
          $('#key-result', main).textContent = 'API key (save this, shown once):\n' + r.key;
        } catch (err) { alert(err.message); }
      });
      $('#logout-btn', main).addEventListener('click', async () => {
        await api('POST', '/api/v1/auth/logout');
        window.location.href = '/login';
      });
    } catch (err) {
      main.innerHTML = '<div class="empty error">' + escapeHTML(err.message) + '</div>';
    }
  }

  function highlightNav(active) {
    $$('.topbar nav a').forEach((a) => {
      if (a.getAttribute('href').startsWith('/' + active)) a.classList.add('active');
    });
  }

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
      try {
        await api('POST', '/api/v1/auth/signup', { tenant_name, email, password });
        window.location.href = '/today';
      } catch (err) { $('#signup-error').textContent = err.message; }
    });
  }
})();
