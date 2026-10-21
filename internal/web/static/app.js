'use strict';

const $ = id => document.getElementById(id);
const categories = ['еда', 'транспорт', 'жильё', 'развлечения', 'здоровье', 'покупки', 'другое'];
const money = value => new Intl.NumberFormat('ru-KZ', { maximumFractionDigits: 2 }).format(Number(value)) + ' ₸';
const dayFormatter = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Almaty', year: 'numeric', month: '2-digit', day: '2-digit' });
const displayDate = value => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Asia/Almaty', day: '2-digit', month: 'short', year: 'numeric' }).format(new Date(value));
function calendarDate(value) {
  const p = Object.fromEntries(dayFormatter.formatToParts(new Date(value)).map(x => [x.type, x.value]));
  return p.year + '-' + p.month + '-' + p.day;
}
const state = { offset: 0, limit: 20, total: 0, items: [], editing: null, deleting: null, saving: false, removing: false, saveUnknown: false, deleteUnknown: false, controller: null, toastTimer: null };
function option(value, label) { const o = document.createElement('option'); o.value = value; o.textContent = label; return o; }
for (const category of categories) {
  $('filter-category').append(option(category, category));
  $('payment-category').append(option(category, category));
}

function showError(id, message = '') { $(id).textContent = message; $(id).hidden = !message; }
function notify(message) {
  clearTimeout(state.toastTimer);
  $('toast').textContent = message; $('toast').hidden = false;
  state.toastTimer = setTimeout(() => { $('toast').hidden = true; }, 5000);
}
function setView(view) {
  for (const id of ['loading', 'error-state', 'empty', 'table-wrap']) $(id).hidden = id !== view;
  $('overview').setAttribute('aria-busy', String(view === 'loading'));
}
function setPeriod() {
  const period = $('period').value;
  if (period === 'month') {
    const today = calendarDate(Date.now());
    $('from').value = today.slice(0, 8) + '01';
    const [year, month] = today.split('-').map(Number);
    $('to').value = today.slice(0, 8) + String(new Date(Date.UTC(year, month, 0)).getUTCDate()).padStart(2, '0');
  } else if (period === 'all') { $('from').value = ''; $('to').value = ''; }
}
function periodText() {
  const from = $('from').value, to = $('to').value;
  const format = d => d.split('-').reverse().join('.');
  if (from && to) return format(from) + ' — ' + format(to) + ' · Алматы';
  if (from) return 'С ' + format(from) + ' · Алматы';
  if (to) return 'По ' + format(to) + ' · Алматы';
  return 'За всё время';
}

async function loadUsers() {
  try {
    const users = await api('/v1/users'), selected = $('filter-user').value;
    $('filter-user').replaceChildren(option('', 'Все участники'));
    $('known-users').replaceChildren();
    for (const user of users) { $('filter-user').append(option(user, 'ID ' + user)); $('known-users').append(option(user, 'Участник ' + user)); }
    if (selected && !users.includes(selected)) $('filter-user').append(option(selected, 'ID ' + selected));
    $('filter-user').value = selected;
    $('users-warning').hidden = true;
  } catch { $('users-warning').hidden = false; }
}

async function loadPayments() {
  state.controller?.abort();
  const controller = new AbortController(); state.controller = controller;
  const from = $('from').value, to = $('to').value;
  if (from && to && from > to) {
    showError('filter-error', 'Начало периода не может быть позже окончания.');
    $('previous').disabled = true; $('next').disabled = true;
    setView('empty');
    for (const id of ['total-amount','total-count','total-members']) $(id).textContent = '—';
    $('page-label').textContent = 'Проверьте даты'; $('period-label').textContent = 'Некорректный период';
    return;
  }
  showError('filter-error');
  setView('loading'); $('previous').disabled = true; $('next').disabled = true;
  $('page-label').textContent = 'Загрузка…';
  for (const id of ['total-amount','total-count','total-members']) $(id).textContent = '—';
  const query = new URLSearchParams({ limit: state.limit, offset: state.offset, timezone: 'Asia/Almaty' });
  for (const [key, value] of new FormData($('filters'))) if (value) query.set(key, value);
  try {
    const result = await api('/v1/payments?' + query, { signal: controller.signal });
    if (controller !== state.controller) return;
    if (state.offset && !result.items.length && result.total) {
      state.offset = Math.floor((result.total - 1) / state.limit) * state.limit; return loadPayments();
    }
    state.items = result.items; state.total = result.total;
    if (!result.total) state.offset = 0;
    $('total-amount').textContent = money(result.amount);
    $('total-count').textContent = result.total.toLocaleString('ru-RU');
    $('total-members').textContent = result.members.toLocaleString('ru-RU');
    $('period-label').textContent = periodText();
    renderRows();
    setView(result.items.length ? 'table-wrap' : 'empty');
    $('page-label').textContent = result.total ? (state.offset + 1) + '–' + (state.offset + result.items.length) + ' из ' + result.total : '0 расходов';
    $('previous').disabled = state.offset === 0;
    $('next').disabled = state.offset + state.limit >= result.total;
  } catch (error) {
    if (error.name === 'AbortError' || controller !== state.controller) return;
    $('load-error').textContent = error.message;
    $('period-label').textContent = 'Данные недоступны';
    $('page-label').textContent = 'Не удалось загрузить данные'; setView('error-state');
  }
}

function element(tag, className, text) {
  const node = document.createElement(tag); if (className) node.className = className;
  if (text !== undefined) node.textContent = text; return node;
}
function renderRows() {
  $('rows').replaceChildren();
  for (const payment of state.items) {
    const row = document.createElement('tr');
    const title = element('td');
    title.append(element('span', 'expense-title', payment.comment || 'Без описания'), element('span', 'badge', payment.category));
    const member = element('td', 'member');
    member.append(element('span', 'member-label', 'Участник'), document.createTextNode(payment.user_id));
    const actions = element('td', 'actions-cell'), buttons = element('div', 'row-actions');
    const edit = element('button', 'quiet', 'Изменить');
    edit.setAttribute('aria-label', 'Изменить расход: ' + (payment.comment || payment.category));
    edit.addEventListener('click', () => openEditor(payment));
    const remove = element('button', 'quiet delete', 'Удалить');
    remove.setAttribute('aria-label', 'Удалить расход: ' + (payment.comment || payment.category));
    remove.addEventListener('click', () => openDelete(payment));
    buttons.append(edit, remove); actions.append(buttons);
    row.append(title, member, element('td', 'date', displayDate(payment.spent_at)), element('td', 'number amount', money(payment.amount)), actions);
    $('rows').append(row);
  }
}

function openEditor(payment = null) {
  state.saveUnknown = false; $('save').disabled = false; $('check-save').hidden = true;
  state.editing = payment; $('payment-form').reset(); showError('save-error');
  $('editor-title').textContent = payment ? 'Редактировать расход' : 'Новый расход';
  $('editor-help').textContent = payment ? 'Изменения появятся в панели и в личных тратах участника.' : 'Заполните детали — запись появится в общей истории.';
  $('save').textContent = payment ? 'Сохранить изменения' : 'Добавить расход';
  $('payment-user').value = payment?.user_id || $('filter-user').value;
  $('payment-user').disabled = Boolean(payment);
  $('owner-hint').textContent = payment ? 'Владелец существующей записи не меняется.' : 'Введите ID или выберите участника из списка.';
  $('payment-amount').value = payment?.amount ?? '';
  $('payment-date').value = payment ? calendarDate(payment.spent_at) : calendarDate(Date.now());
  $('payment-category').value = payment?.category || $('filter-category').value;
  $('payment-comment').value = payment?.comment || '';
  $('editor').showModal();
  (payment ? $('payment-amount') : $('payment-user')).focus();
}
function closeEditor() { if (!state.saving) $('editor').close(); }
$('editor').addEventListener('cancel', event => { if (state.saving) event.preventDefault(); });
$('payment-form').addEventListener('submit', async event => {
  event.preventDefault(); if (state.saving || state.saveUnknown || !$('payment-form').reportValidity()) return;
  const amount = Number($('payment-amount').value);
  if (!Number.isFinite(amount) || amount <= 0) { showError('save-error', 'Укажите положительную сумму.'); return; }
  const original = state.editing;
  const date = $('payment-date').value;
  const body = { amount, category: $('payment-category').value, comment: $('payment-comment').value,
    spent_at: original && calendarDate(original.spent_at) === date ? original.spent_at : date + 'T00:00:00+05:00' };
  if (!original) body.user_id = $('payment-user').value.trim();
  state.saving = true; $('save').disabled = true; $('save').textContent = 'Сохраняем…'; showError('save-error');
  try {
    await api(original ? '/v1/payments/' + original.id : '/v1/payments', { method: original ? 'PATCH' : 'POST', body: JSON.stringify(body) });
    $('editor').close(); state.offset = 0;
    notify(original ? 'Изменения сохранены.' : 'Расход добавлен. Если его нет в списке, проверьте фильтры.');
    await Promise.all([loadPayments(), loadUsers()]);
  } catch (error) {
    state.saveUnknown = Boolean(error.unknownResult);
    $('check-save').hidden = !state.saveUnknown;
    showError('save-error', error.message);
  }
  finally { state.saving = false; $('save').disabled = state.saveUnknown; $('save').textContent = original ? 'Сохранить изменения' : 'Добавить расход'; }
});

function openDelete(payment) {
  state.deleteUnknown = false; $('confirm-delete').disabled = false; $('check-delete').hidden = true;
  state.deleting = payment; showError('delete-error');
  $('delete-description').textContent = payment.comment || payment.category;
  $('delete-amount').textContent = money(payment.amount);
  $('delete-dialog').showModal(); $('cancel-delete').focus();
}
function closeDelete() { if (!state.removing) $('delete-dialog').close(); }
$('delete-dialog').addEventListener('cancel', event => { if (state.removing) event.preventDefault(); });
$('confirm-delete').addEventListener('click', async () => {
  if (state.removing || state.deleteUnknown || !state.deleting) return;
  state.removing = true; $('confirm-delete').disabled = true; $('confirm-delete').textContent = 'Удаляем…'; showError('delete-error');
  try {
    await api('/v1/payments/' + state.deleting.id, { method: 'DELETE' });
    $('delete-dialog').close(); notify('Расход удалён.');
    await Promise.all([loadPayments(), loadUsers()]);
  } catch (error) {
    state.deleteUnknown = Boolean(error.unknownResult);
    $('check-delete').hidden = !state.deleteUnknown;
    showError('delete-error', error.message);
  }
  finally { state.removing = false; $('confirm-delete').disabled = state.deleteUnknown; $('confirm-delete').textContent = 'Удалить расход'; }
});

for (const [button, dialog] of [['check-save', 'editor'], ['check-delete', 'delete-dialog']]) {
  $(button).addEventListener('click', () => {
    $(dialog).close();
    // A verification request only reads; clear filters that could hide the result.
    $('filters').reset(); $('period').value = 'all'; setPeriod(); state.offset = 0;
    loadPayments(); loadUsers();
  });
}

for (const id of ['add','empty-add']) $(id).addEventListener('click', () => openEditor());
for (const id of ['close-editor','cancel-editor']) $(id).addEventListener('click', closeEditor);
for (const id of ['close-delete','cancel-delete']) $(id).addEventListener('click', closeDelete);
$('filters').addEventListener('submit', event => { event.preventDefault(); state.offset = 0; loadPayments(); });
$('filters').addEventListener('change', event => {
  if (event.target.id === 'period') setPeriod();
  else if (event.target.id === 'from' || event.target.id === 'to') $('period').value = 'custom';
  state.offset = 0; loadPayments();
});
$('reset').addEventListener('click', () => {
  $('filters').reset(); $('period').value = 'all'; setPeriod(); state.offset = 0; loadPayments();
});
for (const id of ['refresh','retry']) $(id).addEventListener('click', () => { loadPayments(); loadUsers(); });
$('previous').addEventListener('click', () => { state.offset = Math.max(0, state.offset - state.limit); loadPayments(); });
$('next').addEventListener('click', () => { state.offset += state.limit; loadPayments(); });
setPeriod();
loadUsers();
loadPayments();
