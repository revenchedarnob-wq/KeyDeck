'use strict';

const byId = (id) => document.getElementById(id);
const state = { busy: false };

function setConnection(ok, text) {
  const pill = byId('connection-pill');
  pill.textContent = text;
  pill.className = `pill ${ok ? 'ok' : 'bad'}`;
}

function showError(message) {
  const banner = byId('error-banner');
  banner.textContent = message;
  banner.classList.remove('hidden');
}

function clearError() {
  const banner = byId('error-banner');
  banner.textContent = '';
  banner.classList.add('hidden');
}

async function api(path, options = {}) {
  const response = await fetch(path, { cache: 'no-store', credentials: 'same-origin', ...options });
  const payload = await response.json().catch(() => ({ error: 'Invalid renderer response.' }));
  if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`);
  return payload;
}

function renderTasks(tasks) {
  const list = byId('tasks-list');
  list.replaceChildren();
  byId('tasks-empty').classList.toggle('hidden', tasks.length !== 0);
  for (const task of tasks) {
    const card = document.createElement('article');
    card.className = 'task-card';
    const top = document.createElement('div'); top.className = 'task-card-top';
    const id = document.createElement('div'); id.className = 'task-id'; id.textContent = task.task_id;
    const status = document.createElement('span'); status.className = 'task-status'; status.textContent = task.status;
    top.append(id, status);
    const track = document.createElement('progress'); track.className = 'task-progress'; track.max = 100; track.value = Math.max(0, Math.min(100, task.progress_percent));
    const meta = document.createElement('div'); meta.className = 'task-meta';
    const checks = document.createElement('span'); checks.textContent = `${task.passed_checks}/${task.total_checks} checks`;
    const pct = document.createElement('span'); pct.textContent = `${Math.round(task.progress_percent)}%`;
    meta.append(checks, pct);
    card.append(top, track, meta);
    list.append(card);
  }
}

function renderTimeline(events) {
  const list = byId('timeline-list');
  list.replaceChildren();
  byId('timeline-empty').classList.toggle('hidden', events.length !== 0);
  for (const event of events) {
    const row = document.createElement('li');
    const seq = document.createElement('span'); seq.className = 'timeline-seq'; seq.textContent = `#${event.sequence}`;
    const body = document.createElement('div');
    const kind = document.createElement('div'); kind.className = 'timeline-kind'; kind.textContent = event.kind;
    body.append(kind);
    if (event.summary) {
      const summary = document.createElement('div'); summary.className = 'timeline-summary'; summary.textContent = event.summary; body.append(summary);
    }
    row.append(seq, body); list.append(row);
  }
}

function render(snapshot) {
  byId('task-count').textContent = String(snapshot.status.task_count);
  byId('timeline-count').textContent = String(snapshot.status.timeline_events);
  byId('request-count').textContent = String(snapshot.status.request_records);
  byId('build-id').textContent = snapshot.build_id;
  renderTasks(snapshot.tasks);
  renderTimeline(snapshot.timeline);
  setConnection(true, 'Connected');
}

async function refresh() {
  clearError();
  try { render(await api('api/snapshot?after=0&limit=100')); }
  catch (error) { setConnection(false, 'Disconnected'); showError(error.message); }
}

async function reconnect() {
  clearError();
  try { await api('api/reconnect', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }); await refresh(); }
  catch (error) { setConnection(false, 'Disconnected'); showError(error.message); }
}

byId('refresh-button').addEventListener('click', refresh);
byId('reconnect-button').addEventListener('click', reconnect);
byId('task-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (state.busy) return;
  state.busy = true; byId('create-button').disabled = true; clearError();
  const taskId = byId('task-id').value.trim();
  const payload = {
    idempotency_key: `ui-create-${taskId}`,
    task: {
      task_id: taskId,
      session_id: byId('session-id').value.trim(),
      contract: {
        goal: byId('goal').value.trim(),
        checks: [{ id: 'user-acceptance', description: byId('acceptance-check').value.trim(), status: 'pending' }]
      }
    }
  };
  try {
    await api('api/tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    event.target.reset(); await refresh();
  } catch (error) { showError(error.message); }
  finally { state.busy = false; byId('create-button').disabled = false; }
});

refresh();
