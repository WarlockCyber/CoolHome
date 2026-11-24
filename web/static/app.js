(() => {
  'use strict';

  const REFRESH_MS = 8000;
  const TOKEN_KEY = 'coolhome.token';

  const LABELS = {
    switch: 'Питание',
    switch_1: 'Питание',
    switch_led: 'Свет',
    bright_value: 'Яркость',
    work_mode: 'Режим',
    mode: 'Режим',
    temp_set: 'Целевая температура',
    temp_current: 'Температура',
    va_temperature: 'Температура',
    va_humidity: 'Влажность',
    humidity: 'Влажность',
    battery_percentage: 'Заряд батареи',
    percent_control: 'Открытие',
    control: 'Управление',
    countdown_1: 'Таймер',
    // Sensors ship their data-point code in `name`, so these labels are what
    // the wall actually shows. Without them the panel reads like a protocol
    // dump ("maxtemp_set") instead of settings.
    temp_unit_convert: 'Единицы',
    maxtemp_set: 'Макс. температура',
    minitemp_set: 'Мин. температура',
    maxhum_set: 'Макс. влажность',
    minihum_set: 'Мин. влажность',
    temp_periodic_report: 'Отчёт: температура',
    hum_periodic_report: 'Отчёт: влажность',
    temp_sensitivity: 'Чувствительность т',
    hum_sensitivity: 'Чувствительность вл',
    temp_alarm: 'Сигнал отклонения',
    hum_alarm: 'Сигнал влажности',
    upper_temp: 'Верхний порог',
    lower_temp: 'Нижний порог',
    upper_alarm_temp: 'Сигнал: верх',
    lower_alarm_temp: 'Сигнал: низ',
    upper_temp_switch: 'Следить за верхом',
    lower_temp_switch: 'Следить за низом',
    temp_correction: 'Коррекция',
    cold_delay: 'Задержка холода',
    cold_delay_switch: 'Задержка холод',
    frost: 'Защита от заморозки',
    child_lock: 'Блокировка',
    factory_reset: 'Сброс',
    sensor_choose: 'Датчик',
    switch_all: 'Питание (всё)',
    battery_state: 'Состояние батареи',
    upper_temp_f: 'Верхний порог °F',
    lower_temp_f: 'Нижний порог °F',
    // A learned RF switch exposes one key; the cloud calls it "one ON", which
    // says nothing about what the physical remote's button does (usually a
    // toggle), so the label stays neutral.
    one_on: 'Кнопка',
  };

  const VALUE_LABELS = {
    white: 'белый',
    colour: 'цветной',
    color: 'цветной',
    open: 'открыть',
    stop: 'стоп',
    close: 'закрыть',
    on: 'вкл',
    off: 'выкл',
    auto: 'авто',
    manual: 'вручную',
    // Values sensors and thermostats report: without these the panel shows a
    // protocol token ("cancel", "middle") where a word belongs.
    cancel: 'нет',
    alarm: 'тревога',
    middle: 'средний',
    low: 'низкий',
    high: 'высокий',
    comfort: 'комфорт',
    eco: 'эко',
    frost: 'антизаморозка',
    normal: 'норма',
  };

  const state = {
    devices: [],
    lastSync: null,
    error: '',
    ir: false,
    // layout is the remote: the controls the user chose, in their order.
    // An empty layout means "not configured yet" and shows every device, so an
    // existing install keeps working exactly as before.
    layout: [],
    layoutEditable: false,
    maxWidgets: 200,
    editMode: false,
    // draft is the working copy while editing; null when not editing.
    draft: null,
    dragIndex: null,
    // The device whose settings are open in the modal, or null. Kept here, not
    // in the DOM, so the eight-second refresh cannot close it under a finger.
    sheet: null,
    // The add-controls picker shares the modal; this flag says which of the two
    // the modal currently shows.
    picker: false,
    saving: false,
    token: localStorage.getItem(TOKEN_KEY) || '',
    suspendUntil: 0,
    pending: new Set(),
  };

  const els = {
    grid: document.getElementById('devices'),
    sync: document.getElementById('sync'),
    banner: document.getElementById('banner'),
    empty: document.getElementById('empty'),
    refresh: document.getElementById('refresh'),
    customize: document.getElementById('customize'),
    active: document.getElementById('active'),
    activeList: document.getElementById('active-list'),
    activeCount: document.getElementById('active-count'),
    sheet: document.getElementById('sheet'),
    sheetTitle: document.getElementById('sheet-title'),
    sheetBody: document.getElementById('sheet-body'),
  };

  // ---------- transport ----------

  async function request(path, options = {}) {
    const headers = Object.assign({}, options.headers || {});
    if (state.token) headers.Authorization = 'Bearer ' + state.token;
    if (options.body) headers['Content-Type'] = 'application/json';

    const res = await fetch(path, Object.assign({}, options, { headers }));
    if (res.status === 401) {
      const entered = window.prompt('Введите токен доступа CoolHome');
      if (entered && entered.trim()) {
        state.token = entered.trim();
        localStorage.setItem(TOKEN_KEY, state.token);
        return request(path, options);
      }
      throw new Error('Доступ запрещён: нужен токен');
    }
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      throw new Error((data && data.error) || 'Ошибка HTTP ' + res.status);
    }
    return data;
  }

  async function load({ silent = false } = {}) {
    try {
      const [data, board] = await Promise.all([
        request('/api/devices'),
        request('/api/dashboard').catch(() => null),
      ]);
      state.devices = Array.isArray(data.devices) ? data.devices : [];
    state.ir = Boolean(data.ir);
      if (board && Array.isArray(board.widgets)) {
        // While editing, the draft is authoritative: a refresh must not throw
        // away unsaved work.
        if (!state.editMode) state.layout = board.widgets;
        state.layoutEditable = Boolean(board.editable);
        state.maxWidgets = Number(board.max_widgets) || 200;
      }
      state.lastSync = data.last_sync || null;
      state.error = data.error || '';
    } catch (err) {
      state.error = err.message;
      if (!silent) console.error(err);
    }
    render();
  }

  async function send(deviceId, command) {
    const device = state.devices.find((d) => d.id === deviceId);
    if (!device) return;
    const key = deviceId + ':' + command.code;
    if (state.pending.has(key)) return;

    state.pending.add(key);
    applyLocal(device, command);
    render();
    try {
      const data = await request('/api/devices/' + encodeURIComponent(deviceId) + '/commands', {
        method: 'POST',
        body: JSON.stringify({ commands: [command] }),
      });
      if (data && data.device) replaceDevice(data.device);
      state.error = '';
    } catch (err) {
      state.error = err.message;
      // Resync so an optimistic value never outlives a rejected command.
      load({ silent: true });
    } finally {
      state.pending.delete(key);
    }
    render();
  }

  // ---------- state helpers ----------

  function replaceDevice(next) {
    const i = state.devices.findIndex((d) => d.id === next.id);
    if (i >= 0) state.devices[i] = next;
  }

  function applyLocal(device, command) {
    if (!Array.isArray(device.status)) device.status = [];
    const entry = device.status.find((s) => s.code === command.code);
    if (entry) {
      entry.value = command.value;
    } else {
      device.status.push({ code: command.code, value: command.value });
    }
  }

  function findPoint(deviceId, code) {
    const device = state.devices.find((d) => d.id === deviceId);
    if (!device) return null;
    return (device.functions || []).find((f) => f.code === code) || null;
  }

  // ---------- formatting ----------

  const esc = (value) =>
    String(value).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;',
    }[c]));

  function label(point) {
    // Tuya reports the data-point code in `name` for some products, so a name
    // equal to the code is no name at all and the dictionary wins.
    const name = point.name && point.name !== point.code ? point.name : '';
    return LABELS[point.code] || name || point.code;
  }

  function asNumber(value) {
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  }

  function format(raw, point) {
    if (raw === undefined || raw === null || raw === '') return '—';
    const type = String((point && point.type) || '').toLowerCase();
    if (type === 'boolean') {
      return raw === true || raw === 1 || raw === '1' ? 'вкл' : 'выкл';
    }
    const n = asNumber(raw);
    if (n !== null) {
      const scale = Number(point && point.scale) || 0;
      // Tuya reports brightness on a 10..1000 scale; show it as a percentage.
      if (point && point.code === 'bright_value' && Number.isFinite(point.min) && point.max > point.min) {
        return Math.round(((n - point.min) / (point.max - point.min)) * 100) + '%';
      }
      const scaled = n / Math.pow(10, scale);
      const text = scale > 0 ? scaled.toFixed(scale) : String(Math.round(scaled));
      return point.unit ? text + ' ' + point.unit : text;
    }
    if (type === 'enum') return VALUE_LABELS[raw] || String(raw);
    return String(raw);
  }

  // ---------- rendering ----------

  function render() {
    if (Date.now() < state.suspendUntil) return;

    // Rebuilding the grid would drop keyboard focus; remember the control and
    // restore it on its replacement.
    const active = document.activeElement;
    let restore = null;
    if (active && els.grid.contains(active) && active.dataset && active.dataset.code) {
      restore = {
        id: active.dataset.id,
        code: active.dataset.code,
        action: active.dataset.action || '',
        value: active.dataset.value || '',
      };
    }

    els.banner.hidden = !state.error;
    els.banner.textContent = state.error || '';

    els.sync.textContent = state.lastSync
      ? 'Обновлено в ' + new Date(state.lastSync).toLocaleTimeString('ru-RU')
      : 'Нет данных';
    els.refresh.disabled = false;

    // An open modal is repainted, not closed: the refresh must keep showing it
    // with fresh values.
    if (state.picker) renderPicker();
    else if (state.sheet) renderSheet();

    // The remote is edited in place: the grid is the editor. Adding happens
    // from the tile at the end, removing from each card's own ✕.
    if (state.layout.length > 0 || state.editMode) {
      const total = state.layout.length;
      const widgets = state.layout
        .map((w, i) => {
          const html = widget(w);
          return state.editMode ? wrapEdit(html, i, total, w) : html;
        })
        .join('');
      els.grid.innerHTML = widgets + (state.editMode ? addTile() : '');
      els.grid.classList.add('grid--remote');
    } else {
      els.grid.innerHTML = state.devices.map(card).join('');
      els.grid.classList.remove('grid--remote');
    }
    renderActive();
    els.grid.classList.toggle('grid--editing', state.editMode);
    els.empty.hidden = state.editMode || state.devices.length > 0 || Boolean(state.error);
    els.customize.textContent = state.editMode ? 'Готово' : 'Изменить';
    els.customize.setAttribute('aria-pressed', state.editMode ? 'true' : 'false');

    if (restore) {
      const next = Array.from(els.grid.querySelectorAll('[data-code]')).find(
        (el) =>
          el.dataset.id === restore.id &&
          el.dataset.code === restore.code &&
          (el.dataset.action || '') === restore.action &&
          (el.dataset.value || '') === restore.value,
      );
      if (next) next.focus({ preventScroll: true });
    }
  }

  // ---------- what is on right now ----------

  // The rule for "switched on" lives in power.js so it can be tested without a
  // browser; see the comment there for why not every true boolean counts.
  const power = window.CoolHomePower || { isPowerCode: () => false, powerCode: () => null };

  // powerState returns the switch a device is currently on with, or null. A
  // device with several relays counts only if at least one of them is on.
  function powerState(device) {
    const code = power.powerCode(device);
    if (!code) return null;
    const fn = (device.functions || []).find((f) => f.code === code) || { code: code };
    return { code: code, name: label(fn) };
  }

  // activeDevices lists what is on, and deliberately says nothing about scenes:
  // a scene is an action, not a state, so there is no "on" to report.
  function activeDevices() {
    const out = [];
    for (const device of state.devices) {
      // Only devices that are reachable right now. An offline device's last
      // state may be stale — it could have lost power — and the panel is meant
      // to say what is on, not what might be.
      if (!device.online) continue;
      const power = powerState(device);
      if (power) out.push({ device: device, power: power });
    }
    return out;
  }

  function renderActive() {
    const active = activeDevices();
    // The panel is for the remote; while editing it only gets in the way, and
    // a scene list has nothing to report.
    if (!active.length || state.editMode) {
      els.active.hidden = true;
      els.activeList.innerHTML = '';
      return;
    }
    els.active.hidden = false;
    els.activeCount.textContent = String(active.length);
    els.activeList.innerHTML = active
      .map(({ device, power }) =>
        '<li>' +
          '<button type="button" class="chip" data-active="' + esc(device.id) + '"' +
            ' aria-label="' + esc((device.name || device.id) + ': открыть настройки') + '">' +
            '<span class="chip-name">' + esc(device.name || device.id) + '</span>' +
            '<span class="chip-sub">' + esc(power.name) + '</span>' +
          '</button>' +
        '</li>',
      )
      .join('');
  }

  // The point that carries the measured temperature. Tuya uses two names for
  // it and reports the unit three ways ("℃", "°C", "摄氏度") or not at all, so
  // the code is what identifies it, not the unit.
  const TEMPERATURE_CODES = ['temp_current', 'va_temperature'];

  function isTemperatureCode(code) {
    // A "temp_current_f" is the same reading in Fahrenheit: showing it beside
    // the Celsius value would double every sensor on the wall.
    if (String(code).endsWith('_f')) return false;
    return TEMPERATURE_CODES.indexOf(String(code)) >= 0;
  }

  // isTemperaturePoint identifies the measured temperature by its code alone.
  // The unit cannot decide this: set-points ("maxtemp_set", "upper_temp") are
  // reported in the same Celsius unit, and treating them as sensors put the
  // same control on the wall twice — once as a figure, once as a slider.
  function isTemperaturePoint(point) {
    return Boolean(point) && isTemperatureCode(point.code);
  }

  // temperatureUnit normalises Tuya's spellings to one symbol.
  function temperatureUnit(point) {
    const unit = String((point && point.unit) || '');
    if (unit.indexOf('℉') >= 0 || unit.indexOf('F') >= 0) return '°F';
    return '°C';
  }

  // temperatureNumber renders the value alone, without the unit: the unit sits
  // beside it in smaller type, so a single string would set both at one size.
  function temperatureNumber(raw, point) {
    const n = asNumber(raw);
    if (n === null) return '—';
    const scale = Number(point && point.scale) || 0;
    const value = n / Math.pow(10, scale);
    return scale > 0 ? value.toFixed(scale) : String(Math.round(value));
  }

  // findControl resolves a widget to its device and thing-model function. A
  // widget can outlive the device it points at — the user removes a lamp, or
  // scenes are turned off — so the miss is returned rather than thrown.
  function findControl(deviceID, code) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return null;
    // A writable function first, then a reported reading: the measured
    // temperature is a reading and never appears in functions.
    const point =
      (device.functions || []).find((f) => f.code === code) ||
      (device.readings || []).find((r) => r.code === code);
    if (!point) return null;
    const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));
    return { device, point, raw: values[code], writable: (device.functions || []).some((f) => f.code === code) };
  }

  // widget renders one control of the remote. The title is what the user
  // recognises on the wall ("Кухня", "Свет"), and the control sits under it at
  // a size comfortable for a finger.
  // chrome renders the little controls a widget carries while the page is in
  // edit mode: remove, reorder, width. They are drawn only in edit mode, so a
  // stray tap on the wall cannot delete a card.
  function chrome(index, total, w) {
    return (
      '<div class="widget-edit">' +
        '<button type="button" data-edit="up" data-index="' + index + '"' +
          (index === 0 ? ' disabled' : '') + ' aria-label="Левее">←</button>' +
        '<button type="button" data-edit="down" data-index="' + index + '"' +
          (index === total - 1 ? ' disabled' : '') + ' aria-label="Правее">→</button>' +
        '<button type="button" data-edit="span" data-index="' + index + '"' +
          ' aria-label="Ширина: ' + (w.span === 2 ? 'две колонки' : 'одна колонка') + '">' +
          (w.span === 2 ? '2×' : '1×') + '</button>' +
        '<button type="button" class="danger" data-edit="remove" data-index="' + index + '"' +
          ' aria-label="Убрать с пульта">✕</button>' +
      '</div>'
    );
  }

  // wrapEdit adds the editing chrome around a widget's own markup.
  function wrapEdit(html, index, total, w) {
    return '<div class="slot">' + html + chrome(index, total, w) + '</div>';
  }

  function widget(w) {
    const found = findControl(w.device_id, w.code);
    const span = w.span === 2 ? ' widget--wide' : '';
    if (!found) {
      return (
        '<article class="widget widget--missing' + span + '">' +
          '<div class="widget-title">' + esc(w.title || w.device_id) + '</div>' +
          '<p class="widget-note">Контрол недоступен: устройство или функция исчезли.</p>' +
        '</article>'
      );
    }
    // A sensor widget shows the reading, not a control: the number is what one
    // walks past a wall tablet to read. Everything writable moves behind a
    // disclosure so the figure stays uncluttered.
    if (isTemperaturePoint(found.point) && !found.writable) {
      return sensorWidget(w, found, span);
    }

    const type = String(found.point.type || '').toLowerCase();
    // For a switch or a scene button the device name is the whole label. A
    // slider keeps its function name — unless the widget title already says it,
    // in which case repeating it wastes a line on the wall.
    const fnName = label(found.point);
    const title = w.title || found.device.name || fnName;
    const bare = type === 'boolean' || type === 'button' || title === fnName;
    const inner = control(found.device, found.point, found.raw, {
      bare,
      // A button widget already carries its name as the title; repeating it on
      // the button itself says nothing about what pressing will do.
      label: type === 'button' ? 'Запустить' : bare ? title : fnName,
    });
    return (
      '<article class="widget' + span +
        (found.device.online ? '' : ' widget--offline') + '"' +
        ' data-widget="' + esc(w.device_id + ':' + w.code) + '">' +
        '<div class="widget-title">' + esc(title) + '</div>' +
        inner +
        (found.device.online ? '' : '<div class="widget-off">офлайн</div>') +
      '</article>'
    );
  }

  // sensorWidget renders a measurement large and keeps every writable control
  // of the same device behind a disclosure. The value is the point of the
  // widget; the settings are what one reaches for occasionally.
  function sensorWidget(w, found, span) {
    const device = found.device;
    const key = w.device_id + ':' + w.code;
    const title = w.title || device.name || label(found.point);
    const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));
    return (
      '<article class="widget widget--sensor' + span +
        (device.online ? '' : ' widget--offline') + '"' +
        ' data-widget="' + esc(key) + '">' +
        '<div class="widget-title">' + esc(title) + '</div>' +
        sensorBody(device, found.point, values) +
        (device.online ? '' : '<div class="widget-off">офлайн</div>') +
      '</article>'
    );
  }


  // candidateControls lists everything that could become a widget: every
  // function of every device the UI can actually drive.
  function candidateControls() {
    const out = [];
    for (const device of state.devices) {
      const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));

      // The measured temperature is a reading, not a function: offer it as its
      // own kind of widget so a sensor can sit on the wall as a large figure.
      for (const reading of device.readings || []) {
        if (!isTemperaturePoint(reading)) continue;
        out.push({
          device_id: device.id,
          code: reading.code,
          title: device.name || label(reading),
          device: device.name || device.id,
          point: reading,
          raw: values[reading.code],
          sensor: true,
        });
      }

      for (const point of device.functions || []) {
        const type = String(point.type || '').toLowerCase();
        const known =
          type === 'boolean' ||
          type === 'button' ||
          ((type === 'integer' || type === 'range' || type === 'number') &&
            Number.isFinite(point.min) &&
            Number.isFinite(point.max)) ||
          (type === 'enum' && Array.isArray(point.options) && point.options.length > 0);
        if (!known) continue;
        // A remote with several keys needs them told apart: "Кондер" five
        // times is a picker nobody can use.
        const buttons = (device.functions || []).filter(
          (f) => String(f.type || '').toLowerCase() === 'button',
        ).length;
        const base = device.name || label(point);
        const title = buttons > 1 ? base + ' · ' + label(point) : base;
        out.push({
          device_id: device.id,
          code: point.code,
          title,
          device: device.name || device.id,
          device_category: device.category || '',
          point,
          raw: values[point.code],
        });
      }
    }
    return out;
  }

  // defaultTitle names a widget so that two controls of the same device stay
  // distinguishable on the wall: "Кофемашина" for a lone control, but
  // "Кофемашина · Таймер" when the device offers several.
  function defaultTitle(deviceID, code) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return '';
    const point =
      (device.functions || []).find((f) => f.code === code) ||
      (device.readings || []).find((r) => r.code === code);
    const name = device.name || deviceID;
    // A temperature reading is the device's headline, so it keeps the plain
    // name; a control beside several others gets the function appended.
    const many = (device.functions || []).length > 1 && !isTemperaturePoint(point);
    if (!many || !point) return name;
    const fn = label(point);
    return fn && fn !== name ? name + ' · ' + fn : name;
  }

  function layoutHas(deviceID, code) {
    return state.layout.some((w) => w.device_id === deviceID && w.code === code);
  }

  // addTile is the empty card at the end of the remote: the one place a new
  // control is added from, without leaving the page.
  function addTile() {
    if (state.layout.length >= state.maxWidgets) return '';
    // An empty remote is just this tile, which looks like a mistake unless it
    // says what to do.
    const hint =
      state.layout.length === 0
        ? '<p class="add-hint">Пульт пуст. Нажмите «＋», чтобы выбрать контролы.</p>'
        : '';
    return (
      '<button type="button" class="widget widget--add" data-edit="pick"' +
        ' aria-label="Добавить контрол">' +
        '<span class="add-plus">＋</span>' +
        '<span class="add-label">Добавить</span>' +
      '</button>' + hint
    );
  }

  // pickList is the body of the picker: every control not already placed.
  function pickList() {
    const rest = candidateControls().filter((c) => !layoutHas(c.device_id, c.code));
    if (!rest.length) {
      return '<p class="widget-note">Все доступные контролы уже на пульте.</p>';
    }
    const items = rest
      .map((c) => {
        // A scene's function is named after the scene, so repeating the label
        // under the title says nothing. Name the kind instead.
        const kind = String(c.point.type || '').toLowerCase();
        const sub =
          kind === 'button' ? (c.device_category === 'scene' ? 'Сцена' : 'Кнопка') : label(c.point);
        return (
          '<li class="pick">' +
            '<div class="pick-main">' +
              '<span class="pick-name">' + esc(c.title) + '</span>' +
              '<span class="pick-sub">' + esc(sub) + '</span>' +
            '</div>' +
            '<div class="pick-actions">' +
              '<button type="button" class="ghost" data-edit="add"' +
                ' data-device="' + esc(c.device_id) + '" data-code="' + esc(c.code) + '"' +
                ' aria-label="Добавить на пульт">＋</button>' +
            '</div>' +
          '</li>'
        );
      })
      .join('');
    return '<ul class="picks">' + items + '</ul>';
  }

  // openPicker shows the add list in the same modal used for settings, so the
  // page never grows a second kind of overlay.
  function openPicker() {
    state.picker = true;
    renderPicker();
    if (!els.sheet.open && typeof els.sheet.showModal === 'function') {
      els.sheet.showModal();
    }
  }

  function renderPicker() {
    els.sheetTitle.textContent = 'Добавить на пульт';
    els.sheetBody.classList.remove('sheet-body');
    els.sheetBody.className = 'sheet-body sheet-picker';
    els.sheetBody.innerHTML = pickList();
  }

  // noteFor explains why a card has no widgets. A device the cloud refuses to
  // describe is not a fault, so it must not be reported as one; and the reason
  // differs by category — an RF switch is driven by a remote, while a sensor is
  // simply not exposed, and telling the user to "use its remote" for a
  // thermometer would be wrong.
  function noteFor(device) {
    // A device-level note from the server wins: it is the most specific thing
    // known about why this device cannot be driven.
    if (device.note) {
      return device.note;
    }
    if (device.spec_unsupported) {
      const category = String(device.category || '');
      if (category === 'wnykq' || category === 'ir' || category === 'infrared') {
        // The blaster itself carries no data points: its content is the remotes,
        // which CoolHome serves as separate devices.
        if (state.ir) {
          return 'ИК-пульт: data-point модели у него нет. Его пульты показаны отдельными устройствами в этом списке.';
        }
        return 'ИК-пульт: data-point модели у него нет. Включите COOLHOME_IR=true и подпишите сервис IR Control в консоли Tuya, чтобы увидеть его пульты отдельными устройствами.';
      }
      if (category.indexOf('rf_switch') === 0) {
        return 'RF-выключатель, привязанный к ИК-пульту: у него нет data-point модели, поэтому Tuya Cloud не отдаёт его функции и состояние. Управление — с физического пульта или в приложении Tuya.';
      }
      return 'Tuya Cloud не отдаёт функции и состояние этого устройства через API — оно доступно только в приложении Tuya.';
    }
    if (device.spec_error) {
      return 'Нет описания функций: ' + device.spec_error;
    }
    return 'Нет доступных функций.';
  }

  // devicePanel renders everything a device can offer except the reading that
  // is already shown large: the other measurements, then the writable controls.
  // Fahrenheit duplicates are dropped for the same reason as on the remote.
  function devicePanel(device, values, excludeCode) {
    const parts = [];
    const fnCodes = new Set((device.functions || []).map((f) => f.code));
    for (const r of device.readings || []) {
      if (r.code === excludeCode) continue;
      if (String(r.code).endsWith('_f')) continue;
      if (fnCodes.has(r.code)) continue;
      parts.push(reading(r, values[r.code]));
    }
    for (const f of device.functions || []) {
      if (String(f.code).endsWith('_f')) continue;
      const html = control(device, f, values[f.code], {});
      if (html) parts.push(html);
    }
    return parts.join('');
  }

  // sensorBody is the big figure plus the disclosure that holds everything
  // else. Shared by the card list and the hand-placed widget so a sensor looks
  // the same either way.
  function sensorBody(device, readingPoint, values) {
    const key = device.id + ':' + readingPoint.code;
    const panel = devicePanel(device, values, readingPoint.code);
    // The settings open in a modal, so this is a plain button: a disclosure
    // inside a card cannot escape the card's stacking context, and the next
    // cards in the grid painted over it.
    const more = panel
      ? '<button type="button" class="widget-more" data-more="' + esc(device.id) + '"' +
          ' data-code="' + esc(readingPoint.code) + '"' +
          ' aria-haspopup="dialog">Настройки</button>'
      : '';
    return (
      '<div class="sensor-value">' +
        '<span class="sensor-number">' +
          esc(temperatureNumber(values[readingPoint.code], readingPoint)) +
        '</span>' +
        '<span class="sensor-unit">' + esc(temperatureUnit(readingPoint)) + '</span>' +
      '</div>' +
      more
    );
  }

  function card(device) {
    const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));
    const functions = device.functions || [];
    const fnCodes = new Set(functions.map((f) => f.code));

    const controls = functions.map((f) => control(device, f, values[f.code])).filter(Boolean).join('');
    const readings = (device.readings || [])
      .filter((r) => !fnCodes.has(r.code))
      .map((r) => reading(r, values[r.code]))
      .join('');

    // A device whose headline is a measured temperature is shown as a figure
    // with its settings folded away — not as a list of sliders. Doing this only
    // for hand-placed widgets left every sensor on the plain screen unchanged.
    const tempPoint = (device.readings || []).find(
      (r) => isTemperaturePoint(r) && !fnCodes.has(r.code),
    );
    const sensor = Boolean(tempPoint);

    const hasPassive = functions.some((f) => String(f.type || '').toLowerCase() === 'passive');
    const note =
      (!sensor && !controls && !readings) || hasPassive
        ? '<p class="note">' + esc(noteFor(device)) + '</p>'
        : '';

    const stateClass = device.online ? 'state--on' : 'state--off';
    const stateText = device.online ? 'онлайн' : 'не в сети';

    return (
      '<article class="card' + (sensor ? ' card--sensor' : '') +
        (device.online ? '' : ' card--offline') + '">' +
        '<div class="card-head">' +
          '<div>' +
            '<h2 class="card-title">' + esc(device.name || device.id) + '</h2>' +
            '<p class="card-sub">' + esc(device.product || device.category || 'устройство') + '</p>' +
          '</div>' +
          '<span class="state ' + stateClass + '"><span class="dot"></span>' + stateText + '</span>' +
        '</div>' +
        (sensor
          ? sensorBody(device, tempPoint, values)
          : (controls ? '<div class="controls">' + controls + '</div>' : '') +
            (readings ? '<div class="readings">' + readings + '</div>' : '')) +
        note +
      '</article>'
    );
  }

  // control renders one control. `opts.label` overrides the caption (a widget
  // already shows the device name above it) and `opts.bare` drops the caption
  // entirely for switch/button widgets, where the title says it all.
  function control(device, point, raw, opts) {
    const options = opts || {};
    const type = String(point.type || '').toLowerCase();
    const disabled = device.online ? '' : ' disabled';
    const name = esc(options.label !== undefined ? options.label : label(point));
    const dataAttrs =
      ' data-id="' + esc(device.id) + '" data-code="' + esc(point.code) + '"';

    if (type === 'boolean') {
      const on = raw === true || raw === 1 || raw === '1' || raw === 'true';
      const caption = options.bare ? '' : '<span class="row-label">' + name + '</span>';
      return (
        '<div class="row' + (options.bare ? ' row--bare' : '') + '">' +
          caption +
          '<button type="button" class="switch" role="switch" aria-checked="' + on + '"' +
            ' aria-label="' + name + (on ? ', включено' : ', выключено') + '"' +
            ' data-action="toggle"' + dataAttrs + disabled + '>' +
            '<span class="knob"></span>' +
          '</button>' +
        '</div>'
      );
    }

    if (type === 'integer' || type === 'range' || type === 'number') {
      const min = Number.isFinite(point.min) ? point.min : 0;
      const max = Number.isFinite(point.max) ? point.max : 100;
      const step = Number(point.step) > 0 ? point.step : 1;
      const value = asNumber(raw);
      const clamped = value === null ? min : Math.min(Math.max(value, min), max);
      const outId = 'out-' + device.id + '-' + point.code;
      const head =
        '<div class="row-head">' +
          (options.bare ? '' : '<span class="row-label">' + name + '</span>') +
          '<span class="row-value" id="' + esc(outId) + '">' + esc(format(raw, point)) + '</span>' +
        '</div>';
      return (
        '<div class="row--slider">' +
          head +
          '<input type="range" min="' + min + '" max="' + max + '" step="' + step + '"' +
            ' value="' + clamped + '" aria-label="' + name + '"' +
            ' data-action="range" data-out="' + esc(outId) + '"' + dataAttrs + disabled + '>' +
        '</div>'
      );
    }

    // An infrared remote key: a momentary action, not a state. It is rendered
    // as a plain button and always writes true ("press").
    if (type === 'button') {
      return (
        '<div class="row' + (options.bare ? ' row--bare' : '') + '">' +
          '<button type="button" class="press" data-action="press"' + dataAttrs + disabled + '>' +
            name +
          '</button>' +
        '</div>'
      );
    }

    if (type === 'enum' && Array.isArray(point.options) && point.options.length > 0) {
      const buttons = point.options
        .map((opt) => {
          const active = String(raw) === opt;
          return (
            '<button type="button" aria-pressed="' + active + '"' +
              ' data-action="set" data-value="' + esc(opt) + '"' + dataAttrs + disabled + '>' +
              esc(VALUE_LABELS[opt] || opt) +
            '</button>'
          );
        })
        .join('');
      return (
        '<div>' +
          (options.bare ? '' : '<div class="row-head"><span class="row-label">' + name + '</span></div>') +
          '<div class="segmented" role="group" aria-label="' + name + '">' + buttons + '</div>' +
        '</div>'
      );
    }

    // String/JSON data points have no safe generic control; the cloud rejects
    // guesses anyway. They stay out of the UI instead of pretending to work.
    return '';
  }

  function reading(point, raw) {
    return (
      '<div class="reading">' +
        '<span class="reading-label">' + esc(label(point)) + '</span>' +
        '<span class="reading-value">' + esc(format(raw, point)) + '</span>' +
      '</div>'
    );
  }

  // ---------- events ----------

  els.grid.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const button = event.target.closest('button[data-action]');
    if (!button) return;
    const { action, id, code, value } = button.dataset;
    if (action === 'toggle') {
      send(id, { code, value: button.getAttribute('aria-checked') !== 'true' });
    } else if (action === 'set') {
      if (button.getAttribute('aria-pressed') !== 'true') send(id, { code, value });
    } else if (action === 'press') {
      // A key press is always new: pressing the same button twice must send
      // twice, so nothing here is deduplicated by value.
      send(id, { code, value: true });
    }
  });

  els.grid.addEventListener('input', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    const out = document.getElementById(input.dataset.out);
    if (!out) return;
    const point = findPoint(input.dataset.id, input.dataset.code);
    if (point) out.textContent = format(Number(input.value), point);
  });

  els.grid.addEventListener('change', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    state.suspendUntil = 0;
    send(input.dataset.id, { code: input.dataset.code, value: Number(input.value) });
  });

  els.grid.addEventListener('pointerdown', (event) => {
    if (event.target instanceof HTMLInputElement && event.target.type === 'range') {
      state.suspendUntil = Date.now() + 2000;
    }
  });

  document.addEventListener('pointerup', () => {
    if (state.suspendUntil) {
      state.suspendUntil = 0;
      render();
    }
  });

  els.refresh.addEventListener('click', async () => {
    els.refresh.disabled = true;
    try {
      await request('/api/refresh', { method: 'POST' });
      state.error = '';
    } catch (err) {
      state.error = err.message;
    }
    await load({ silent: true });
  });

  // ---------- editing in place ----------

  // Every edit is saved as it happens. A wall tablet has no "save" moment: the
  // user taps ✕, walks away, and expects it to stay gone.
  let saveTimer = 0;

  function persistLayout() {
    if (!state.layoutEditable) {
      // Nothing to persist to: keep the change in memory for this session and
      // say so rather than pretend it survived.
      state.error = 'Раскладка хранится только в памяти: задайте COOLHOME_DASHBOARD_FILE.';
      render();
      return;
    }
    clearTimeout(saveTimer);
    saveTimer = setTimeout(async () => {
      state.saving = true;
      try {
        const data = await request('/api/dashboard', {
          method: 'PUT',
          body: JSON.stringify({ widgets: state.layout }),
        });
        // The canonical form comes back: spans clamped, order kept.
        if (data && Array.isArray(data.widgets)) state.layout = data.widgets;
        state.error = '';
      } catch (err) {
        state.error = err.message;
      } finally {
        state.saving = false;
        render();
      }
    }, 250);
  }

  function startEditing() {
    state.editMode = true;
    render();
  }

  function stopEditing() {
    state.editMode = false;
    if (state.picker) closeSheet();
    render();
  }

  // applyEdit mutates the layout for one of the card buttons.
  function applyEdit(action, index, button) {
    const layout = state.layout;
    if (action === 'remove' && index >= 0 && index < layout.length) {
      layout.splice(index, 1);
    } else if (action === 'up' && index > 0) {
      const [moved] = layout.splice(index, 1);
      layout.splice(index - 1, 0, moved);
    } else if (action === 'down' && index >= 0 && index < layout.length - 1) {
      const [moved] = layout.splice(index, 1);
      layout.splice(index + 1, 0, moved);
    } else if (action === 'span' && index >= 0 && index < layout.length) {
      layout[index].span = layout[index].span === 2 ? 1 : 2;
    } else if (action === 'add' && button) {
      if (layout.length >= state.maxWidgets) return;
      const deviceID = button.dataset.device;
      const code = button.dataset.code;
      if (layoutHas(deviceID, code)) return;
      layout.push({ device_id: deviceID, code: code, title: defaultTitle(deviceID, code), span: 1 });
    } else {
      return;
    }
    persistLayout();
    render();
  }

  // ---------- settings modal ----------

  // openSheet shows one device's settings in a modal. A modal is used instead
  // of a panel inside the card because an absolutely positioned child cannot
  // rise above the sibling cards that follow it in the grid.
  function openSheet(deviceID, code) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return;
    state.picker = false;
    state.sheet = { deviceID: deviceID, code: code };
    renderSheet();
    if (!els.sheet.open && typeof els.sheet.showModal === 'function') {
      els.sheet.showModal();
    }
  }

  function closeSheet() {
    state.sheet = null;
    state.picker = false;
    if (els.sheet.open) els.sheet.close();
    els.sheetBody.innerHTML = '';
  }

  // renderSheet repaints the open modal from current state, so a refresh that
  // arrives while it is open updates the values instead of closing it.
  function renderSheet() {
    if (!state.sheet) return;
    const device = state.devices.find((d) => d.id === state.sheet.deviceID);
    if (!device) {
      els.sheetTitle.textContent = 'Устройство недоступно';
      els.sheetBody.innerHTML = '<p class="widget-note">Устройство исчезло из дома.</p>';
      return;
    }
    const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));
    const point = (device.readings || []).find((r) => r.code === state.sheet.code);
    const unit = point ? temperatureUnit(point) : '';
    const figure = point
      ? '<div class="sensor-value sheet-figure">' +
          '<span class="sensor-number">' + esc(temperatureNumber(values[point.code], point)) + '</span>' +
          '<span class="sensor-unit">' + esc(unit) + '</span>' +
        '</div>'
      : '';
    const panel = devicePanel(device, values, state.sheet.code);
    els.sheetTitle.textContent = device.name || device.id;
    els.sheetBody.innerHTML =
      figure +
      (panel ? '<div class="sheet-controls">' + panel + '</div>' : '<p class="widget-note">Нет настроек.</p>');
  }

  els.activeList.addEventListener('click', (event) => {
    const chip = event.target instanceof Element ? event.target.closest('[data-active]') : null;
    if (!chip) return;
    openSheet(chip.dataset.active, null);
  });

  els.grid.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest('[data-more]') : null;
    if (!button) return;
    openSheet(button.dataset.more, button.dataset.code);
  });

  els.sheet.addEventListener('click', (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest('[data-sheet="close"]')) {
      closeSheet();
      return;
    }
    // A click on the backdrop lands on the dialog element itself.
    if (target === els.sheet) closeSheet();
  });

  // Esc closes natively; forget the state too so the next render does not
  // reopen it.
  els.sheet.addEventListener('close', () => {
    state.sheet = null;
    state.picker = false;
  });

  // Controls inside the modal are handled here, because the grid handlers are
  // bound to the grid element and the dialog is a sibling.
  els.sheetBody.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;

    // The add list lives in the modal too: picking a control puts it on the
    // remote and closes the list, so one tap is the whole action.
    const add = event.target.closest('[data-edit="add"]');
    if (add) {
      applyEdit('add', -1, add);
      closeSheet();
      return;
    }

    const button = event.target.closest('button[data-action]');
    if (!button) return;
    const { action, id, code, value } = button.dataset;
    if (action === 'toggle') {
      send(id, { code, value: button.getAttribute('aria-checked') !== 'true' });
    } else if (action === 'set') {
      if (button.getAttribute('aria-pressed') !== 'true') send(id, { code, value });
    } else if (action === 'press') {
      send(id, { code, value: true });
    }
  });

  els.sheetBody.addEventListener('change', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    state.suspendUntil = 0;
    send(input.dataset.id, { code: input.dataset.code, value: Number(input.value) });
  });

  els.sheetBody.addEventListener('input', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    const out = document.getElementById(input.dataset.out);
    if (!out) return;
    const point = findPoint(input.dataset.id, input.dataset.code);
    if (point) out.textContent = format(Number(input.value), point);
  });

  els.customize.addEventListener('click', () => {
    if (state.editMode) stopEditing();
    else startEditing();
  });

  els.grid.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest('[data-edit]') : null;
    if (!button) return;
    const action = button.dataset.edit;
    if (action === 'pick') {
      openPicker();
      return;
    }
    const index = Number(button.dataset.index);
    applyEdit(action, Number.isInteger(index) ? index : -1, button);
  });

  setInterval(() => {
    if (!document.hidden) load({ silent: true });
  }, REFRESH_MS);

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) load({ silent: true });
  });

  // A bookmark can open the editor directly: on a wall tablet the address bar
  // is hard to reach, so "?edit=1" saves a trip through the settings button.
  load().then(() => {
    if (new URLSearchParams(location.search).get('edit') === '1') startEditing();
  });
})();
