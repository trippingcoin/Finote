'use strict';

const API_TIMEOUT_MS = 12000;
const UNKNOWN_OUTCOME = 'Не удалось подтвердить результат операции. Проверьте список расходов перед повторной отправкой.';

function requestError(message, unknownResult = false) {
  const error = new Error(message);
  error.unknownResult = unknownResult;
  return error;
}

// One deadline includes receiving and parsing the body. Never retries mutations.
async function api(path, options = {}, timeoutMs = API_TIMEOUT_MS) {
  const mutation = !['GET', 'HEAD'].includes((options.method || 'GET').toUpperCase());
  const controller = new AbortController();
  const external = options.signal;
  const abort = () => controller.abort();
  let timedOut = false;
  if (external?.aborted) abort();
  else external?.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(() => { timedOut = true; controller.abort(); }, timeoutMs);
  try {
    const response = await fetch(path, {
      ...options, signal: controller.signal,
      headers: { 'Content-Type': 'application/json', ...options.headers }
    });
    if (response.status === 204) return null;
    let data;
    try { data = await response.json(); }
    catch (error) {
      if (controller.signal.aborted) throw error;
      throw requestError(mutation ? UNKNOWN_OUTCOME : 'Сервер вернул некорректный ответ. Попробуйте обновить страницу.', mutation);
    }
    if (!response.ok) {
      const unknown = mutation && (response.status >= 500 || response.status === 408);
      throw requestError(unknown ? UNKNOWN_OUTCOME : (data?.error || 'Не удалось выполнить запрос.'), unknown);
    }
    // A missing/malformed success payload cannot confirm a saved expense.
    if (mutation && (!data || typeof data.id !== 'string')) throw requestError(UNKNOWN_OUTCOME, true);
    return data;
  } catch (error) {
    if (timedOut) throw requestError(mutation ? UNKNOWN_OUTCOME : 'Сервер не ответил вовремя. Попробуйте обновить список.', mutation);
    if (external?.aborted && !mutation) {
      const canceled = new Error('Запрос отменён.'); canceled.name = 'AbortError'; throw canceled;
    }
    if (typeof error.unknownResult === 'boolean') throw error;
    throw requestError(mutation ? UNKNOWN_OUTCOME : 'Нет связи с сервером. Проверьте подключение и попробуйте снова.', mutation);
  } finally {
    clearTimeout(timer);
    external?.removeEventListener('abort', abort);
  }
}
