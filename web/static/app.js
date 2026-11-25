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
    // Humidity as the thing model spells it (v2 models, not the legacy spec).
    humidity_value: 'Влажность',
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
    // Settings behind a plug's card. They are readable now that the card itself
    // only carries the on/off state.
    relay_status: 'Состояние после включения',
    light_mode: 'Подсветка кнопки',
    overcharge_switch: 'Защита от перегрузки',
    cycle_time: 'Цикл вкл/выкл',
    random_time: 'Случайная задержка',
    switch_inching: 'Импульсный режим',
    online_state: 'Сеть',
    switch_type: 'Тип реле',
    switch_interlock: 'Блокировка реле',
    alarm_set_1: 'Сигнал 1',
    alarm_set_2: 'Сигнал 2',
    charge_energy: 'Заряд энергии',
    remote_add: 'Пульт',
    remote_list: 'Пульты',
    holiday_days_set: 'Дней отпуска',
    countdown_set: 'Таймер',
    // Electrical readings of a metering plug.
    add_ele: 'Израсходовано',
    cur_current: 'Ток',
    cur_power: 'Мощность',
    cur_voltage: 'Напряжение',
    total_energy: 'Всего энергии',
    voltage_coe: 'Коэффициент напряжения',
    electric_coe: 'Коэффициент тока',
    power_coe: 'Коэффициент мощности',
    electricity_coe: 'Коэффициент учёта',
    fault: 'Неисправность',
    // A learned RF switch exposes one key; the cloud calls it "one ON", which
    // says nothing about what the physical remote's button does (usually a
    // toggle), so the label stays neutral.
    one_on: 'Кнопка',
    // Thermostats paired through a gateway describe themselves through the
    // thing model, where every protection setting arrives under its protocol
    // code. Without names these read as a dump: "ovp", "ocp", "cryogenic".
    heating_temp_start: 'Старт нагрева',
    heating_temp_stop: 'Стоп нагрева',
    cryogenic_temp_start: 'Старт охлаждения',
    cryogenic_temp_stop: 'Стоп охлаждения',
    temp_calibration: 'Калибровка',
    ovp: 'Защита: напряжение',
    ocp: 'Защита: ток',
    opp: 'Защита: мощность',
    lvp: 'Защита: низкое напряжение',
    over_protect_enable: 'Защита от перегрузки',
    over_protect_time: 'Время защиты',
    real_time_swith_1s_60s: 'Обновление в реальном времени',
    over_time: 'Задержка перегрева',
    warning: 'Предупреждение',
    total_electricity: 'Всего энергии',
    electricity_price: 'Цена энергии',
    cost: 'Стоимость',
    sensor_detection_flag: 'Датчик температуры',
    countdown_recovery: 'Возврат таймера',
  };

  // Some enums use numeric option values whose meaning depends on the data
  // point: relay_status 0/1/2 is off/on/remember, while level 1/2 is just a
  // number. A global map on the raw value would mistranslate one of them.
  const OPTION_LABELS = {
    relay_status: { 0: 'выкл', 1: 'вкл', 2: 'как было' },
    // Celsius/fahrenheit as a sensor reports the setting, not as a letter.
    temp_unit_convert: { c: '°C', f: '°F' },
    switch_type: { flip: 'переключатель', sync: 'синхронно', button: 'кнопка' },
    light_mode: { relay: 'реле', pos: 'положение', none: 'выкл' },
  };

  function optionLabel(point, option) {
    const byCode = OPTION_LABELS[point && point.code];
    if (byCode && Object.prototype.hasOwnProperty.call(byCode, option)) {
      return byCode[option];
    }
    return VALUE_LABELS[option] || String(option);
  }

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
    // Relay behaviour after a power cut.
    power_off: 'выкл',
    power_on: 'вкл',
    last: 'как было',
    memory: 'как было',
    off_remember: 'как было',
    // Indicator light of a plug's button.
    relay: 'реле',
    pos: 'положение',
    none: 'выкл',
    // Relay modes and countdown presets.
    flip: 'переключатель',
    sync: 'синхронно',
    button: 'кнопка',
    online: 'в сети',
    offline: 'не в сети',
    '1h': '1 ч',
    '2h': '2 ч',
    '3h': '3 ч',
    '4h': '4 ч',
    '5h': '5 ч',
    '6h': '6 ч',
    // Values sensors and thermostats report: without these the panel shows a
    // protocol token ("cancel", "middle") where a word belongs.
    cancel: 'нет',
    alarm: 'тревога',
    middle: 'средний',
    low: 'низкий',
    high: 'высокий',
    comfort: 'комфорт',
    eco: 'эко',
    // Work modes of a thermostat behind a gateway.
    socket: 'розетка',
    hot: 'нагрев',
    colding: 'охлаждение',
    frost: 'антизаморозка',
    normal: 'норма',
  };

  // The bundle this page was rendered from. The server reports its own on every
  // poll; when they differ, a restart brought new code and the page reloads.
  // Without this a dashboard left open on a wall keeps running the interface it
  // was loaded with, and a fixed card goes on looking broken for days.
  const BUILD = (document.querySelector('meta[name="coolhome-build"]') || {}).content || '';

  const state = {
    devices: [],
    lastSync: null,
    // error is transient: a failed request, cleared by the next good one.
    error: '',
    // notice is a condition that outlives a refresh — the cloud refusing all
    // commands because the project's plan is out of controllable devices. A
    // banner that vanishes on the next poll reads as "nothing happened".
    notice: '',
    // problem is the message shown in a modal: a condition with a place to go
    // and fix it. Kept here so a repaint cannot close it under a finger.
    problem: null,
    // mutedProblems remembers the codes the user asked not to see again in this
    // session. Session only, not stored: a wall tablet that never shows the
    // message again would hide a condition that may still be there tomorrow.
    mutedProblems: new Set(),
    // rules is the automation screen: the set, the runner's state, the journal.
    // null until the first load, which is how the page knows to say "загрузка".
    rules: null,
    // ruleDraft is the rule being edited, or null. Held here, not in the DOM, so
    // the eight-second refresh cannot close it under a finger.
    ruleDraft: null,
    ir: false,
    // layout is the remote: the controls the user chose, in their order.
    // An empty layout means "not configured yet" and shows every device, so an
    // existing install keeps working exactly as before.
    layout: [],
    layoutEditable: false,
    maxWidgets: 200,
    // panel is the side column: device ids, in the order the user put them
    // there. A card, not a widget: it carries every control of its device.
    panel: [],
    maxPanel: 12,
    // names are this installation's device names, keyed by device id. Empty
    // means the cloud's name is shown, which is the state before anybody
    // renames anything.
    names: {},
    maxNameLen: 60,
    editMode: false,
    // draft is the working copy while editing; null when not editing.
    draft: null,
    dragIndex: null,
    // The device whose settings are open in the modal, or null. Kept here, not
    // in the DOM, so the eight-second refresh cannot close it under a finger.
    sheet: null,
    // The picker shares the modal with device settings. Its value says which
    // list it shows: 'controls' adds to the remote, 'panel' adds a device card
    // to the side column. Empty means the modal is not a picker.
    picker: '',
    // Which tab is shown. Scenes live on their own: they are actions without a
    // state, and mixed into the grid they only got in the way of the lights.
    tab: 'devices',
    saving: false,
    token: localStorage.getItem(TOKEN_KEY) || '',
    suspendUntil: 0,
    pending: new Set(),
  };

  const els = {
    grid: document.getElementById('devices'),
    scenes: document.getElementById('scenes'),
    workspace: document.getElementById('workspace'),
    side: document.getElementById('side'),
    sideList: document.getElementById('side-list'),
    sideCount: document.getElementById('side-count'),
    tabs: document.querySelectorAll('[data-tab]'),
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
    rename: document.getElementById('rename'),
    renameInput: document.getElementById('rename-input'),
    renameHint: document.getElementById('rename-hint'),
    renameReset: document.getElementById('rename-reset'),
    renameTitle: document.getElementById('rename-title'),
    problem: document.getElementById('problem'),
    problemTitle: document.getElementById('problem-title'),
    problemMessage: document.getElementById('problem-message'),
    problemLink: document.getElementById('problem-link'),
    problemMute: document.getElementById('problem-mute'),
    rulesPanel: document.getElementById('rules'),
    rulesStatus: document.getElementById('rules-status'),
    rulesGroups: document.getElementById('rules-groups'),
    rulesList: document.getElementById('rules-list'),
    rulesEmpty: document.getElementById('rules-empty'),
    rulesJournal: document.getElementById('rules-journal'),
    rulesJournalTitle: document.getElementById('rules-journal-title'),
    rulesAdd: document.getElementById('rules-add'),
    rulesCheck: document.getElementById('rules-check'),
    ruleEditor: document.getElementById('rule-editor'),
    ruleBody: document.getElementById('rule-body'),
    ruleErrors: document.getElementById('rule-errors'),
    ruleForm: document.getElementById('rule-form'),
    ruleTitle: document.getElementById('rule-editor-title'),
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
      const err = new Error((data && data.error) || 'Ошибка HTTP ' + res.status);
      // The reason travels with the error so the caller can tell a condition it
      // should keep on screen from one a resync will clear. When the server
      // sends a whole message — title, explanation, link — it comes along.
      if (data && data.code) err.code = data.code;
      if (data && (data.title || data.message)) err.problem = data;
      throw err;
    }
    return data;
  }

  async function load({ silent = false } = {}) {
    try {
      const [data, board, ruleSet] = await Promise.all([
        request('/api/devices'),
        request('/api/dashboard').catch(() => null),
        request('/api/rules').catch(() => null),
      ]);
      // A server without the rules feature answers with running:false, which is
      // a real answer; a failed request leaves the previous set in place rather
      // than blanking the screen every time the network hiccups.
      if (ruleSet) state.rules = ruleSet;
      state.devices = Array.isArray(data.devices) ? data.devices : [];
    state.ir = Boolean(data.ir);
      if (board && Array.isArray(board.widgets)) {
        // While editing, the draft is authoritative: a refresh must not throw
        // away unsaved work.
        if (!state.editMode) state.layout = board.widgets;
        state.layoutEditable = Boolean(board.editable);
        state.maxWidgets = Number(board.max_widgets) || 200;
      }
      if (board && Array.isArray(board.panel) && !state.editMode) {
        state.panel = panelList.normalize(board.panel);
      }
      if (board && Number(board.max_panel) > 0) state.maxPanel = Number(board.max_panel);
      // The names ride with the layout: same file, same answer. A missing key
      // is an installation that has renamed nothing, not a failure.
      if (board && board.names && typeof board.names === 'object') state.names = board.names;
      if (board && Number(board.max_name_len) > 0) state.maxNameLen = Number(board.max_name_len);
      state.lastSync = data.last_sync || null;
      state.error = data.error || '';
      // A reload mid-edit would throw away an unsaved layout, so it waits for
      // the user to leave the editor; the next poll tries again.
      // A reload would wipe a name being typed, which is the very thing the
      // rename dialog was moved out of the settings modal for.
      if (data.build && BUILD && data.build !== BUILD &&
          !state.editMode && !state.sheet && !state.picker && !els.rename.open) {
        location.reload();
        return;
      }
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
      // The cloud accepts commands again: whatever it refused before is over.
      state.notice = '';
    } catch (err) {
      if (err.code === 'quota_exhausted') {
        // Not a transient error: every command will be refused until the Tuya
        // plan changes, so the message stays until it does — or until it is
        // dismissed. The device itself is fine, and saying "error" would blame
        // it.
        state.error = '';
        state.notice = '';
        showProblem(err.problem || { code: 'quota_exhausted', message: err.message });
      } else {
        state.error = err.message;
      }
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

  // pointLabel names a data point in the context of its device. A two-gang
  // switch has switch_1 and switch_2, and calling them both "Питание" makes the
  // settings list unusable; the key number is the only thing that tells them
  // apart.
  function pointLabel(point, device) {
    const code = String(point.code || '');
    const relays = (device && device.functions ? device.functions : []).filter((f) =>
      /^switch_\d+$/.test(f.code),
    );
    let m = /^switch_(\d+)$/.exec(code);
    if (m && relays.length > 1) return 'Клавиша ' + m[1];
    m = /^countdown_(\d+)$/.exec(code);
    if (m && relays.length > 1) return 'Таймер ' + m[1];
    return label(point);
  }

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

    // Rebuilding the cards would drop keyboard focus; remember the control and
    // restore it on its replacement. The panel is repainted on the same tick, so
    // a slider there must be remembered too — otherwise the eight-second
    // refresh would take the focus away mid-drag.
    const active = document.activeElement;
    let restore = null;
    if (
      active &&
      (els.grid.contains(active) || els.sideList.contains(active)) &&
      active.dataset &&
      active.dataset.code
    ) {
      restore = {
        id: active.dataset.id,
        code: active.dataset.code,
        action: active.dataset.action || '',
        value: active.dataset.value || '',
        // Which container held it. A device can be drawn in both the grid and
        // the panel, and every key above matches both copies: without this the
        // focus would jump to the other column on the next poll.
        inPanel: els.sideList.contains(active),
      };
    }
    // The panel's own buttons carry no code, so they are remembered by what they
    // do: reordering with the keyboard must not drop the focus to the page.
    const sideAction = active && els.sideList.contains(active) ? active.dataset : null;
    const restoreSide =
      sideAction && sideAction.sideEdit
        ? { action: sideAction.sideEdit, side: sideAction.side || '' }
        : sideAction && sideAction.sideAdd
          ? { action: 'add', side: '' }
          : null;

    // A notice is a condition with a way out, an error is a failure: they look
    // different and the notice can be dismissed.
    const notice = !state.error && state.notice ? state.notice : '';
    els.banner.hidden = !state.error && !notice;
    els.banner.className = notice ? 'banner banner--notice' : 'banner';
    els.banner.innerHTML = notice
      ? '<span class="banner-text">' + esc(notice) + '</span>' +
        '<button type="button" class="banner-close" data-notice="dismiss" aria-label="Скрыть">✕</button>'
      : esc(state.error || '');

    els.sync.textContent = state.lastSync
      ? 'Обновлено в ' + new Date(state.lastSync).toLocaleTimeString('ru-RU')
      : 'Нет данных';
    els.refresh.disabled = false;

    // An open modal is repainted, not closed: the refresh must keep showing it
    // with fresh values.
    if (state.picker) renderPicker();
    else if (state.sheet) renderSheet();

    // Scenes have their own tab. They are filtered out of the device grid and
    // the remote editor: a scene is a button, not a device with a state, and
    // mixing them made both lists worse.
    const scenes = state.devices.filter(isScene);
    const devices = state.devices.filter((d) => !isScene(d));
    els.scenes.innerHTML = scenes.map(card).join('');

    const tabs = { devices: 'devices', scenes: 'scenes', rules: 'rules' };
    for (const name of Object.keys(tabs)) {
      const on = state.tab === name;
      const panel = name === 'devices' ? els.grid : name === 'scenes' ? els.scenes : els.rulesPanel;
      panel.hidden = !on;
      const btn = document.getElementById('tab-' + name);
      if (btn) btn.setAttribute('aria-selected', on ? 'true' : 'false');
    }
    if (state.tab === 'rules') renderRules();
    // The "nothing here" hint belongs to the devices tab, which is the one that
    // shows the whole house.
    const onDevices = state.tab === 'devices';

    // The remote is edited in place: the grid is the editor. Adding happens
    // from the tile at the end, removing from each card's own ✕.
    if (onDevices && (state.layout.length > 0 || state.editMode)) {
      const total = state.layout.length;
      const widgets = state.layout
        .map((w, i) => {
          const html = widget(w);
          return state.editMode ? wrapEdit(html, i, total, w) : html;
        })
        .join('');
      els.grid.innerHTML = widgets + (state.editMode ? addTile() : '');
      els.grid.classList.add('grid--remote');
    } else if (onDevices) {
      els.grid.innerHTML = devices.map(card).join('');
      els.grid.classList.remove('grid--remote');
    }
    renderActive();
    renderPanel();
    els.grid.classList.toggle('grid--editing', state.editMode);
    els.empty.hidden =
      !onDevices || state.editMode || devices.length > 0 || scenes.length > 0 || Boolean(state.error);
    els.customize.textContent = state.editMode ? 'Готово' : 'Изменить';
    els.customize.setAttribute('aria-pressed', state.editMode ? 'true' : 'false');

    if (restore) {
      const primary = restore.inPanel ? els.sideList : els.grid;
      const secondary = restore.inPanel ? els.grid : els.sideList;
      const matches = (el) =>
        el.dataset.id === restore.id &&
        el.dataset.code === restore.code &&
        (el.dataset.action || '') === restore.action &&
        (el.dataset.value || '') === restore.value;
      // The same container first, the twin only as a fallback: a focus that
      // follows the finger must not move to the other column.
      const next =
        Array.from(primary.querySelectorAll('[data-code]')).find(matches) ||
        Array.from(secondary.querySelectorAll('[data-code]')).find(matches);
      if (next) next.focus({ preventScroll: true });
    }
    if (restoreSide) {
      const selector =
        restoreSide.action === 'add'
          ? '[data-side-add]'
          : '[data-side-edit="' + restoreSide.action + '"][data-side="' + restoreSide.side + '"]';
      const next = els.sideList.querySelector(selector);
      // A button that became disabled at the edge is not focusable, so fall back
      // to the one that still works rather than losing the place.
      const usable = next && !next.disabled ? next : els.sideList.querySelector('[data-side-edit], [data-side-add]');
      if (usable) usable.focus({ preventScroll: true });
    }
  }

  // ---------- what is on right now ----------

  // The rule for "switched on" lives in power.js so it can be tested without a
  // browser; see the comment there for why not every true boolean counts.
  const power = window.CoolHomePower || {
    isPowerCode: () => false,
    isOn: () => false,
    powerCode: () => null,
    powerCard: () => null,
  };

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

  // ---------- side panel ----------

  // renderPanel draws the left-hand column: whole device cards, in the order
  // they were placed. It answers a different question than the remote — the
  // remote is what one reaches for, the panel is what one wants to see at a
  // glance — so it is its own list, and it stays put when the tab changes.
  function renderPanel() {
    const ids = state.panel;
    // An empty panel is not shown at all: a permanent empty column beside the
    // grid would be a hole in the layout. While editing it is shown, because
    // that is the only place a card is added from.
    const visible = ids.length > 0 || state.editMode;
    els.side.hidden = !visible;
    els.side.classList.toggle('side-panel--editing', state.editMode);
    els.workspace.classList.toggle('workspace--split', visible);
    if (!visible) {
      els.sideList.innerHTML = '';
      return;
    }
    els.sideCount.textContent = String(ids.length);
    // Devices whose card no longer names anything. Named on the header so the
    // number in the column is not silently larger than what is shown.
    const gone = panelList.missing(ids, state.devices).length;
    els.sideCount.title = gone > 0 ? gone + ' из ' + ids.length + ' недоступны' : '';
    const items = ids
      .map((id, i) => {
        const device = state.devices.find((d) => d.id === id);
        // A device that left the home keeps its slot: removing it silently
        // would reorder the column under a hand reaching for a card. The ids
        // with no device come from panel.js, so the same rule is testable.
        let body = device
          ? card(device)
          : '<div class="side-missing">Устройство исчезло из дома. В режиме правки карточку можно убрать.</div>';
        // While editing, the card is inert: the chrome sits right under it, and
        // a stray tap aimed at ✕ must not toggle a light. The grid has the same
        // rule — editing replaces its cards with the editor, so nothing there
        // can be pressed either.
        if (state.editMode && device) {
          body = '<div class="side-card" inert>' + body + '</div>';
        }
        const chrome = state.editMode ? sideChrome(i, ids.length, id) : '';
        return (
          '<div class="side-item" data-side-id="' + esc(id) + '"' +
            (state.editMode ? ' role="group" aria-label="' + esc(sideName(id)) + '"' : '') + '>' +
            body + chrome +
          '</div>'
        );
      })
      .join('');
    const add = state.editMode && ids.length < state.maxPanel ? sideAddTile(ids.length === 0) : '';
    els.sideList.innerHTML = items + add;
  }

  function sideChrome(index, total, id) {
    // Each label names the device. Twelve cards in a column otherwise read as
    // twelve identical "Выше" in a screen reader.
    const who = sideName(id);
    return (
      '<div class="side-edit">' +
        '<button type="button" data-side-edit="up" data-side="' + esc(id) + '"' +
          (index === 0 ? ' disabled' : '') + ' aria-label="' + esc(who + ': выше') + '">↑</button>' +
        '<button type="button" data-side-edit="down" data-side="' + esc(id) + '"' +
          (index === total - 1 ? ' disabled' : '') + ' aria-label="' + esc(who + ': ниже') + '">↓</button>' +
        '<button type="button" class="danger" data-side-edit="remove" data-side="' + esc(id) + '"' +
          ' aria-label="' + esc(who + ': убрать с панели') + '">✕</button>' +
      '</div>'
    );
  }

  // sideName is what a screen reader calls a panel entry, including one whose
  // device has left the home.
  function sideName(id) {
    const device = state.devices.find((d) => d.id === id);
    return device ? device.name || device.id : 'устройство ' + id;
  }

  // sideAddTile is the panel's empty slot while editing: the one place a card is
  // added from, mirroring the remote's add tile.
  function sideAddTile(empty) {
    const hint = empty
      ? '<p class="side-hint">Панель пуста. «＋» добавит карточку устройства — например термостат или датчик.</p>'
      : '';
    return (
      '<button type="button" class="side-add" data-side-add="1" aria-label="Добавить устройство на панель">' +
        '<span class="add-plus">＋</span><span>Добавить</span>' +
      '</button>' + hint
    );
  }

  // panelPickList is the body of the picker in panel mode: devices, not single
  // controls. A scene is excluded — a scene is an action, and its card in a
  // "what is happening at home" column would say nothing.
  function panelPickList() {
    const rest = state.devices.filter((d) => !isScene(d) && !panelList.has(state.panel, d.id));
    if (!rest.length) {
      return '<p class="widget-note">Все устройства уже на панели.</p>';
    }
    const items = rest
      .map((d) => {
        const sub = d.product || categoryLabel(d) || 'устройство';
        return (
          '<li class="pick">' +
            '<div class="pick-main">' +
              '<span class="pick-name">' + esc(d.name || d.id) + '</span>' +
              '<span class="pick-sub">' + esc(sub) + '</span>' +
            '</div>' +
            '<div class="pick-actions">' +
              '<button type="button" class="ghost" data-side-pick="' + esc(d.id) + '"' +
                ' aria-label="Добавить на панель">＋</button>' +
            '</div>' +
          '</li>'
        );
      })
      .join('');
    return '<ul class="picks">' + items + '</ul>';
  }

  // addToPanel places a device card. The rule (no duplicates, the limit) lives
  // in panel.js; a refusal is reported instead of silently doing nothing.
  function addToPanel(deviceID) {
    const res = panelList.add(state.panel, deviceID, state.maxPanel);
    state.panel = res.panel;
    if (res.error) {
      state.error = 'Не удалось добавить на панель: ' + res.error;
      render();
      return;
    }
    state.error = '';
    persistLayout('panel');
    render();
  }

  function applyPanelEdit(action, id) {
    const index = state.panel.indexOf(id);
    if (action === 'remove') state.panel = panelList.remove(state.panel, id);
    else if (action === 'up') state.panel = panelList.move(state.panel, index, -1);
    else if (action === 'down') state.panel = panelList.move(state.panel, index, 1);
    else return;
    persistLayout('panel');
    render();
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

  // Which readings become figures, and why by code rather than by unit, lives
  // in figures.js: the rule is small enough to look right while being wrong, so
  // it is tested outside the browser.
  const figures = window.CoolHomeFigures;
  const isTemperaturePoint = figures.isTemperaturePoint;
  const isHumidityPoint = figures.isHumidityPoint;

  // The side panel's list rules live in panel.js: duplicates, the limit and the
  // reordering are easy to get subtly wrong, and a wrong one looks like a
  // rendering glitch rather than a bug.
  const panelList = window.CoolHomePanel;

  // Local names: trimming, the bound and which widget titles follow a rename.
  // The server applies the names themselves; these rules are what the dialog
  // uses before anything is sent.
  const localNames = window.CoolHomeNames;

  // The rule form's helpers: value parsing, the sentence a rule reads as, and
  // what stops a draft from being saved.
  const ruleHelp = window.CoolHomeRules;

  // temperatureUnit normalises Tuya's spellings to one symbol.
  function temperatureUnit(point) {
    const unit = String((point && point.unit) || '');
    if (unit.indexOf('℉') >= 0 || unit.indexOf('F') >= 0) return '°F';
    return '°C';
  }

  // figureNumber renders a reading's value alone, without the unit: the unit
  // sits beside it in smaller type, so a single string would set both at one
  // size. It is shared by the temperature and the humidity figures.
  function figureNumber(raw, point) {
    // A point declared but not yet reported arrives as null. Number(null) is 0,
    // which would put a plausible "0 %" on the wall where the settings row says
    // "—"; an unreported value must look unreported.
    if (raw === null || raw === undefined || raw === '') return '—';
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
        '<div class="widget-head">' +
          '<div class="widget-title">' + esc(title) + '</div>' +
          // A configured remote replaces the device grid, so without this the
          // only way into a device's settings — and to renaming it — would be to
          // clear the remote. The gear opens the same modal the cards use.
          '<button type="button" class="widget-gear" data-more="' + esc(found.device.id) + '"' +
            ' data-code="' + esc(w.code) + '" aria-haspopup="dialog"' +
            ' aria-label="' + esc(title + ': настройки') + '">⚙</button>' +
        '</div>' +
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
        // A sensor widget now draws both numbers of a temperature-and-humidity
        // device, so naming only the temperature understates what gets added.
        const paired =
          c.sensor &&
          state.devices.some((d) => d.id === c.device_id && figurePoints(d).humid);
        const sub =
          kind === 'button'
            ? c.device_category === 'scene' ? 'Сцена' : 'Кнопка'
            : paired ? 'Температура и влажность' : label(c.point);
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

  // openPicker shows an add list in the same modal used for settings, so the
  // page never grows a second kind of overlay. `mode` picks the list: single
  // controls for the remote, whole devices for the side panel.
  function openPicker(mode) {
    state.picker = mode === 'panel' ? 'panel' : 'controls';
    renderPicker();
    if (!els.sheet.open && typeof els.sheet.showModal === 'function') {
      els.sheet.showModal();
    }
  }

  function renderPicker() {
    const forPanel = state.picker === 'panel';
    els.sheetTitle.textContent = forPanel ? 'Добавить на панель' : 'Добавить на пульт';
    els.sheetBody.classList.remove('sheet-body');
    els.sheetBody.className = 'sheet-body sheet-picker';
    els.sheetBody.innerHTML = forPanel ? panelPickList() : pickList();
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
  // Calibration coefficients are constants a technician reads once; listing them
  // above a plug's switch buries what the panel was opened for.
  function isCalibration(code) {
    const c = String(code);
    return /_coe$/.test(c) || c === 'fault' || c === 'test_bit';
  }

  // devicePanel renders a device's settings. Order depends on what the panel is
  // for: a plug is opened to change something, so its switch goes first; a
  // sensor is opened to read humidity and battery beside the temperature, so
  // its readings stay on top.
  function devicePanel(device, values, excludeCodes, controlsFirst) {
    // What is already drawn as a figure above must not be drawn again as a row:
    // a temperature-and-humidity sensor shows both on the card, so both leave
    // the panel.
    const skip = new Set(excludeCodes || []);
    const controls = [];
    for (const f of device.functions || []) {
      if (String(f.code).endsWith('_f')) continue;
      const html = control(device, f, values[f.code], {});
      if (html) controls.push(html);
    }

    const fnCodes = new Set((device.functions || []).map((f) => f.code));
    const readings = [];
    for (const r of device.readings || []) {
      if (skip.has(r.code)) continue;
      if (String(r.code).endsWith('_f')) continue;
      if (isCalibration(r.code)) continue;
      if (fnCodes.has(r.code)) continue;
      readings.push(reading(r, values[r.code]));
    }

    return (controlsFirst ? controls.concat(readings) : readings.concat(controls)).join('');
  }

  // figurePoints answers "does this device have figures, and of what kind" for
  // callers that only need to know (the widget picker, the card's layout).
  function figurePoints(device) {
    return figures.figurePoints(device);
  }

  // humidityUnit keeps the cloud's "%" but never shows an empty label where a
  // reading has no unit of its own.
  function humidityUnit(point) {
    const unit = String((point && point.unit) || '');
    return unit || '%';
  }

  // sensorFigures draws the numbers themselves and reports which codes it drew.
  // Returning both is what keeps the card and the settings panel in step: the
  // panel skips exactly what was drawn, so no value can be lost between them.
  function sensorFigures(device, values) {
    const picked = figures.figures(device);
    const codes = picked.codes.slice();
    if (!picked.temp) return { html: '', codes };
    const main =
      '<div class="sensor-value">' +
        '<span class="sensor-number">' + esc(figureNumber(values[picked.temp.code], picked.temp)) + '</span>' +
        '<span class="sensor-unit">' + esc(temperatureUnit(picked.temp)) + '</span>' +
      '</div>';
    if (!picked.humid) return { html: '<div class="sensor-readings">' + main + '</div>', codes };
    const side =
      '<div class="sensor-side">' +
        '<span class="sensor-side-label">Влажность</span>' +
        '<div class="sensor-value">' +
          '<span class="sensor-number">' + esc(figureNumber(values[picked.humid.code], picked.humid)) + '</span>' +
          '<span class="sensor-unit">' + esc(humidityUnit(picked.humid)) + '</span>' +
        '</div>' +
      '</div>';
    return { html: '<div class="sensor-readings">' + main + side + '</div>', codes };
  }

  // sensorBody is the figures plus the disclosure that holds everything else.
  // Shared by the card list, the hand-placed widget and the settings modal so a
  // sensor looks the same everywhere.
  function sensorBody(device, readingPoint, values) {
    const drawn = sensorFigures(device, values);
    if (readingPoint && drawn.codes.indexOf(readingPoint.code) < 0) drawn.codes.push(readingPoint.code);
    const panel = devicePanel(device, values, drawn.codes, false);
    // The settings open in a modal, so this is a plain button: a disclosure
    // inside a card cannot escape the card's stacking context, and the next
    // cards in the grid painted over it.
    const more = panel
      ? '<button type="button" class="widget-more" data-more="' + esc(device.id) + '"' +
          ' data-code="' + esc(readingPoint.code) + '"' +
          ' aria-haspopup="dialog">Настройки</button>'
      : '';
    return drawn.html + more;
  }

  // powerBody is the whole card of a plug or a switch: the state of each key,
  // the switches, and a way into the settings. A plug exposes nine controls —
  // countdown, relay mode, indicator light, child lock, over-charge protection —
  // and none of them belong on a wall; the on/off does.
  //
  // A multi-gang switch gets one row per key, because they are separate lights
  // and the wall has a separate button for each.
  function powerBody(device, codes, values) {
    const name = device.name || device.id;
    const many = codes.length > 1;
    const rows = codes
      .map((code, i) => {
        const on = power.isOn(values[code]);
        const what = many ? 'Клавиша ' + (i + 1) : name;
        const state = on ? 'Включено' : 'Выключено';
        return (
          '<div class="power-state' + (on ? ' power-state--on' : '') + '">' +
            '<span class="power-label">' + (many ? esc(what) + ' · ' + state : state) + '</span>' +
            '<button type="button" class="switch' + (many ? '' : ' switch--xl') + '" role="switch"' +
              ' aria-checked="' + on + '" aria-label="' + esc(what + ': ' + state) + '"' +
              ' data-action="toggle" data-id="' + esc(device.id) + '"' +
              ' data-code="' + esc(code) + '"' +
              (device.online ? '' : ' disabled') + '>' +
              '<span class="knob"></span>' +
            '</button>' +
          '</div>'
        );
      })
      .join('');
    return (
      '<div class="power-rows">' + rows + '</div>' +
      '<button type="button" class="widget-more" data-more="' + esc(device.id) + '"' +
        ' data-code="" aria-haspopup="dialog">Настройки</button>'
    );
  }

  // isScene marks a tap-to-run scene: a button the cloud runs, with no state of
  // its own. Everything else is a device.
  function isScene(device) {
    return String(device.category || '') === 'scene';
  }

  // categoryLabel names the categories CoolHome itself introduces, and leaves
  // the product name alone where the cloud supplies one.
  function categoryLabel(device) {
    const category = String(device.category || '');
    if (category === 'scene') return 'Сценарий';
    if (category === 'ir_remote') return 'ИК-пульт';
    return category;
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

    // A plug or a plain switch collapses to its power state. A thermostat keeps
    // its card: it has a set-point worth seeing. A multi-gang strip keeps its
    // individual switches, because powerCard refuses to pick one of them.
    const powerCodes = sensor ? null : power.powerCard(device);
    const powerOnly = Boolean(powerCodes && powerCodes.length);

    const hasPassive = functions.some((f) => String(f.type || '').toLowerCase() === 'passive');
    // A scene always keeps its button, so the "no controls" rule never fires for
    // it — and a scene switched off in the Tuya app then showed as a grey
    // "Запустить" with no reason given. Its note says why.
    const sceneNote = isScene(device) && Boolean(device.note);
    const note =
      sceneNote || (!sensor && !powerOnly && !controls && !readings) || hasPassive
        ? '<p class="note">' + esc(noteFor(device)) + '</p>'
        : '';

    const stateClass = device.online ? 'state--on' : 'state--off';
    const stateText = device.online ? 'онлайн' : 'не в сети';

    return (
      '<article class="card' + (sensor ? ' card--sensor' : '') +
        (powerOnly ? ' card--power' : '') +
        (isScene(device) ? ' card--scene' : '') +
        (device.online ? '' : ' card--offline') + '">' +
        '<div class="card-head">' +
          '<div>' +
            '<h2 class="card-title">' + esc(device.name || device.id) + '</h2>' +
            '<p class="card-sub">' + esc(device.product || categoryLabel(device) || 'устройство') + '</p>' +
          '</div>' +
          '<span class="state ' + stateClass + '"><span class="dot"></span>' + stateText + '</span>' +
        '</div>' +
        (sensor
          ? sensorBody(device, tempPoint, values)
          : powerOnly
            ? powerBody(device, powerCodes, values)
            : (controls ? '<div class="controls">' + controls + '</div>' : '') +
              (readings ? '<div class="readings">' + readings + '</div>' : '') +
              // Every card leads somewhere: the settings modal is also where a
              // device is renamed, and a device whose controls all fit on the
              // card would otherwise have no way in at all.
              '<button type="button" class="widget-more" data-more="' + esc(device.id) + '"' +
                ' data-code="" aria-haspopup="dialog">Настройки</button>') +
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
    const name = esc(
      options.label !== undefined ? options.label : pointLabel(point, device),
    );
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
      // The reading beside the slider is found from the slider itself, not by
      // an id. The same card can be drawn twice — in the grid and in the side
      // panel — and two elements cannot share an id: a global lookup would then
      // update the copy in the other column while the finger moved this one.
      const head =
        '<div class="row-head">' +
          (options.bare ? '' : '<span class="row-label">' + name + '</span>') +
          '<span class="row-value">' + esc(format(raw, point)) + '</span>' +
        '</div>';
      return (
        '<div class="row--slider">' +
          head +
          '<input type="range" min="' + min + '" max="' + max + '" step="' + step + '"' +
            ' value="' + clamped + '" aria-label="' + name + '"' +
            ' data-action="range"' + dataAttrs + disabled + '>' +
        '</div>'
      );
    }

    // An infrared remote key: a momentary action, not a state. It is rendered
    // as a plain button and always writes true ("press").
    if (type === 'button') {
      // The card title already carries the scene's name; repeating it on the
      // button says nothing about what pressing will do.
      const caption = options.label !== undefined ? options.label : 'Запустить';
      return (
        '<div class="row' + (options.bare ? ' row--bare' : '') + '">' +
          '<button type="button" class="press" data-action="press"' + dataAttrs + disabled + '>' +
            esc(caption) +
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
              esc(optionLabel(point, opt)) +
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

  // A card can be drawn twice — in the grid and in the side panel — so the
  // behaviour is bound to every container that holds cards, not to the grid
  // alone. One implementation, two roots: a control in the panel must work
  // exactly like the same control in the grid.
  function onCardClick(event) {
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
  }

  // valueBeside resolves the reading that belongs to this slider. It walks up
  // to the slider's own row, so a card drawn in two places updates the number
  // next to the finger rather than the first copy in the document.
  function valueBeside(input) {
    const row = input.closest('.row--slider');
    return row ? row.querySelector('.row-value') : null;
  }

  function onCardInput(event) {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    const out = valueBeside(input);
    if (!out) return;
    const point = findPoint(input.dataset.id, input.dataset.code);
    if (point) out.textContent = format(Number(input.value), point);
  }

  function onCardChange(event) {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'range') return;
    state.suspendUntil = 0;
    send(input.dataset.id, { code: input.dataset.code, value: Number(input.value) });
  }

  function onCardPointerDown(event) {
    if (event.target instanceof HTMLInputElement && event.target.type === 'range') {
      state.suspendUntil = Date.now() + 2000;
    }
  }

  function onCardMore(event) {
    if (!(event.target instanceof Element)) return;
    const button = event.target.closest('[data-more]');
    if (!button) return;
    openSheet(button.dataset.more, button.dataset.code);
  }

  for (const root of [els.grid, els.sideList]) {
    root.addEventListener('click', onCardClick);
    root.addEventListener('click', onCardMore);
    root.addEventListener('input', onCardInput);
    root.addEventListener('change', onCardChange);
    root.addEventListener('pointerdown', onCardPointerDown);
  }

  document.addEventListener('pointerup', () => {
    if (state.suspendUntil) {
      state.suspendUntil = 0;
      render();
    }
  });

  els.problem.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    // A click on the backdrop lands on the dialog element itself.
    if (event.target.closest('[data-problem="close"]') || event.target === els.problem) {
      closeProblem();
    }
  });

  // Esc closes natively; the state has to be forgotten too, or the next repaint
  // would reopen it.
  els.problem.addEventListener('close', () => {
    // Esc closes natively through this path, not through closeProblem: the
    // checkbox still means what it says.
    applyProblemMute();
    state.problem = null;
    render();
  });

  els.banner.addEventListener('click', (event) => {
    const button = event.target instanceof Element ? event.target.closest('[data-notice]') : null;
    if (!button) return;
    state.notice = '';
    render();
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

  // persistLayout saves the list that changed. Sending both would let one
  // screen edit undo another: a phone adding a card to the panel and a tablet
  // reordering the remote are two writers to one document, and each must leave
  // the other's list alone. The server treats an absent key as "keep it".
  function persistLayout(which) {
    const changed = which === 'panel' ? 'panel' : 'widgets';
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
        const body =
          changed === 'panel'
            ? { panel: state.panel }
            : { widgets: state.layout };
        const data = await request('/api/dashboard', {
          method: 'PUT',
          body: JSON.stringify(body),
        });
        // The canonical form comes back: spans clamped, order kept. Both lists
        // are taken back, because the answer is the whole document.
        if (data && Array.isArray(data.widgets)) state.layout = data.widgets;
        if (data && Array.isArray(data.panel)) state.panel = panelList.normalize(data.panel);
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

  // ---------- problem dialog ----------

  // showProblem puts a condition the user must act on into a modal. It is used
  // for answers from the cloud that only the Tuya console can fix; a banner is
  // too easy to miss on a wall, and too easy to mistake for a passing error.
  function showProblem(p) {
    if (!p || !p.message) return false;
    const code = p.code || 'problem';
    if (state.mutedProblems.has(code)) {
      // The user asked not to see this again: it stays as the quiet banner.
      state.notice = p.message;
      render();
      return false;
    }
    state.problem = p;
    els.problemTitle.textContent = p.title || 'Нужно действие в консоли Tuya';
    els.problemMessage.textContent = p.message;
    // The link is only rendered when the server sent one, and only if it is a
    // web address: a message must not be able to turn itself into a script.
    const link = typeof p.link === 'string' && /^https?:\/\//i.test(p.link) ? p.link : '';
    els.problemLink.hidden = !link;
    if (link) {
      els.problemLink.href = link;
      els.problemLink.textContent = p.linkLabel || 'Открыть консоль Tuya';
    }
    els.problemMute.checked = false;
    if (!els.problem.open && typeof els.problem.showModal === 'function') {
      els.problem.showModal();
    }
    return true;
  }

  function closeProblem() {
    applyProblemMute();
    state.problem = null;
    if (els.problem.open) els.problem.close();
    render();
  }

  // applyProblemMute remembers "do not show this again" for the session. With
  // the modal gone the condition is still there, so it stays as a quiet banner:
  // hidden from the eye, not from the page.
  function applyProblemMute() {
    if (!els.problemMute.checked || !state.problem || !state.problem.code) return;
    state.mutedProblems.add(state.problem.code);
    state.notice = state.problem.message;
  }

  // ---------- rules ----------

  // ruleLabel names a device and, when a code is given, the point inside it. The
  // conditional part matters: a rule reads "Температура меньше 18", not
  // "bfb7dccc9fe2fa2b9flqge.temp_current < 18".
  function ruleLabel(deviceID, code) {
    const device = state.devices.find((d) => d.id === deviceID);
    const name = device ? device.name || device.id : deviceID;
    if (!code) return name;
    const point = device
      ? (device.functions || []).find((f) => f.code === code) ||
        (device.readings || []).find((r) => r.code === code)
      : null;
    const fn = point ? label(point) : code;
    return fn && fn !== name ? name + ' · ' + fn : name;
  }

  // selectablePoints lists what a condition or an action may name: every
  // reported reading and every writable function, with its current value so the
  // editor can prefill something sensible.
  function selectablePoints(deviceID) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return [];
    const values = Object.fromEntries((device.status || []).map((s) => [s.code, s.value]));
    const seen = new Set();
    const out = [];
    for (const list of [device.functions || [], device.readings || []]) {
      for (const p of list) {
        if (seen.has(p.code) || String(p.code).endsWith('_f')) continue;
        seen.add(p.code);
        out.push({ code: p.code, label: label(p), value: values[p.code] });
      }
    }
    return out;
  }

  // groupEnabled answers whether a group is switched on. A rule naming a group
  // the page has not seen yet is treated as ungrouped, like the server does.
  function groupEnabled(name) {
    const groups = (state.rules && state.rules.groups) || [];
    const found = groups.find((g) => g.name === name);
    return found ? Boolean(found.enabled) : true;
  }

  function renderRules() {
    const data = state.rules;
    if (!data) {
      els.rulesStatus.textContent = 'Загрузка правил…';
      return;
    }
    // The status line answers the one question the list cannot: is anything
    // actually watching the house right now?
    const notes = [];
    if (!data.running) {
      notes.push('Правила выключены: задайте COOLHOME_RULES_FILE (или COOLHOME_RULES=true).');
    } else if (!data.primed) {
      notes.push('Правила ещё не проверялись — первая проверка задаёт точку отсчёта.');
    } else if (data.last_run) {
      notes.push('Проверено в ' + new Date(data.last_run).toLocaleTimeString('ru-RU'));
    }
    if (data.editable === false) {
      notes.push('Список хранится только в памяти: задайте COOLHOME_RULES_FILE.');
    }
    if (data.timezone) notes.push('Часовой пояс: ' + data.timezone + '.');
    if (data.last_error) notes.push('Ошибка: ' + data.last_error);
    els.rulesStatus.textContent = notes.join(' ');

    els.rulesGroups.innerHTML = (data.groups || [])
      .map((g) => {
        const on = Boolean(g.enabled);
        return (
          '<div class="rules-group">' +
            '<span class="rules-group-name">' + esc(g.name) + '</span>' +
            '<button type="button" class="switch" role="switch" aria-checked="' + on + '"' +
              ' aria-label="' + esc(g.name + ': ' + (on ? 'группа включена' : 'группа выключена')) + '"' +
              ' data-rule-group="' + esc(g.name) + '"><span class="knob"></span></button>' +
          '</div>'
        );
      })
      .join('');

    const rules = data.rules || [];
    els.rulesEmpty.hidden = rules.length > 0;
    els.rulesList.innerHTML = rules.map(ruleRow).join('');

    const journal = data.journal || [];
    // A dry run answers "what would happen": it is labelled so it is not read as
    // something the house has just done.
    const preview = journal.some((e) => e.preview);
    els.rulesJournalTitle.hidden = journal.length === 0;
    els.rulesJournalTitle.textContent = preview ? 'Что сработало бы сейчас' : 'Последние срабатывания';
    els.rulesJournal.innerHTML = journal
      .slice()
      .reverse()
      .map((e) => {
        const when = new Date(e.time).toLocaleTimeString('ru-RU');
        // The journal keeps device ids; the page is what knows their names, and
        // which of them the person renamed locally.
        const what = (e.actions || [])
          .map((a) => {
            const name =
              a.kind === 'scene'
                ? 'сцена ' + ruleLabel(a.scene, '')
                : ruleLabel(a.device, a.code) + ' → ' + ruleHelp.formatValue(a.value);
            return a.note ? name + ' (' + a.note + ')' : name;
          })
          .join(', ');
        return (
          '<li class="journal-row' + (e.error ? ' journal-row--error' : '') + '">' +
            '<span class="journal-time">' + esc(when) + '</span>' +
            '<span class="journal-name">' + esc(e.name || e.rule) + '</span>' +
            '<span class="journal-what">' + esc(what) + '</span>' +
          '</li>'
        );
      })
      .join('');
  }

  function ruleRow(rule) {
    const summary = ruleHelp.summarize(rule, ruleLabel);
    const on = Boolean(rule.enabled);
    // A rule can be switched on while its group is off. Saying so is the
    // difference between "this is broken" and "this is paused with the group".
    const groupOff = Boolean(rule.group) && !groupEnabled(rule.group);
    const groupBadge = rule.group
      ? '<span class="rule-group' + (groupOff ? ' rule-group--off' : '') + '">' +
          esc(rule.group) + (groupOff ? ' · выключена' : '') +
        '</span>'
      : '';
    return (
      '<li class="rule' + (on && !groupOff ? '' : ' rule--off') + '" data-rule-row="' + esc(rule.id) + '">' +
        '<div class="rule-main">' +
          '<div class="rule-name">' + esc(rule.name) + groupBadge + '</div>' +
          '<div class="rule-trigger">' + esc(summary.trigger || '—') + '</div>' +
          '<div class="rule-does">' + esc(summary.actions || '—') + '</div>' +
          (summary.notes ? '<div class="rule-notes">' + esc(summary.notes) + '</div>' : '') +
        '</div>' +
        '<div class="rule-side">' +
          '<button type="button" class="switch" role="switch" aria-checked="' + on + '"' +
            ' aria-label="' + esc(rule.name + ': ' + (on ? 'включено' : 'выключено')) + '"' +
            ' data-rule-toggle="' + esc(rule.id) + '"><span class="knob"></span></button>' +
          '<button type="button" class="ghost rule-edit" data-rule-edit="' + esc(rule.id) + '"' +
            ' aria-label="' + esc(rule.name + ': изменить') + '">✎</button>' +
          '<button type="button" class="ghost ghost--danger" data-rule-delete="' + esc(rule.id) + '"' +
            ' aria-label="' + esc(rule.name + ': удалить') + '">✕</button>' +
        '</div>' +
      '</li>'
    );
  }

  // ---------- rule editor ----------

  function emptyDraft() {
    return {
      id: '',
      name: '',
      group: '',
      enabled: true,
      when: [{ device: '', code: '', op: '<', value: '' }],
      at: { from: '', to: '', days: [] },
      for: '',
      cooldown: '',
      mode: 'edge',
      actions: [{ kind: 'command', device: '', code: '', value: '', scene: '' }],
    };
  }

  function openRuleEditor(rule) {
    state.ruleDraft = rule
      ? {
          id: rule.id,
          name: rule.name || '',
          group: rule.group || '',
          enabled: rule.enabled !== false,
          when: (rule.when || []).map((c) => ({ ...c })),
          at: { from: '', to: '', days: [], ...(rule.at || {}) },
          for: rule.for || '',
          cooldown: rule.cooldown || '',
          mode: rule.mode || 'edge',
          actions: (rule.actions || []).map((a) => ({
            kind: a.kind,
            device: a.device || '',
            code: a.code || '',
            value: a.value === undefined || a.value === null ? '' : String(a.value),
            scene: a.scene || '',
          })),
        }
      : emptyDraft();
    if (!state.ruleDraft.when.length && state.ruleDraft.at.from) {
      state.ruleDraft.when = [];
    }
    if (!state.ruleDraft.actions.length) state.ruleDraft.actions = [{ kind: 'command', device: '', code: '', value: '', scene: '' }];
    els.ruleTitle.textContent = rule ? 'Изменить правило' : 'Новое правило';
    els.ruleErrors.hidden = true;
    renderRuleEditor();
    if (!els.ruleEditor.open && typeof els.ruleEditor.showModal === 'function') {
      els.ruleEditor.showModal();
    }
  }

  function closeRuleEditor() {
    state.ruleDraft = null;
    if (els.ruleEditor.open) els.ruleEditor.close();
  }

  function deviceOptions(selected, placeholder) {
    const opts = ['<option value="">' + esc(placeholder) + '</option>'];
    for (const d of state.devices.filter((x) => !isScene(x))) {
      opts.push(
        '<option value="' + esc(d.id) + '"' + (d.id === selected ? ' selected' : '') + '>' +
          esc(d.name || d.id) +
        '</option>',
      );
    }
    return opts.join('');
  }

  function pointOptions(deviceID, selected) {
    const points = selectablePoints(deviceID);
    const opts = ['<option value="">— показание —</option>'];
    for (const p of points) {
      opts.push(
        '<option value="' + esc(p.code) + '"' + (p.code === selected ? ' selected' : '') + '>' +
          esc(p.label) +
        '</option>',
      );
    }
    return opts.join('');
  }

  function sceneOptions(selected) {
    const scenes = state.devices.filter(isScene);
    const opts = ['<option value="">— сцена —</option>'];
    for (const s of scenes) {
      opts.push(
        '<option value="' + esc(s.id) + '"' + (s.id === selected ? ' selected' : '') + '>' +
          esc(s.name || s.id) +
        '</option>',
      );
    }
    return opts.join('');
  }

  function renderRuleEditor() {
    const d = state.ruleDraft;
    if (!d) return;
    // A group is free text with the existing ones offered as suggestions: the
    // group switch is only useful if a group can be made, and requiring a JSON
    // edit for that would make half the feature unreachable from the page.
    const groupNames = ((state.rules && state.rules.groups) || [])
      .map((g) => '<option value="' + esc(g.name) + '"></option>')
      .join('');

    const conditions = d.when
      .map((c, i) => {
        const points = selectablePoints(c.device);
        const isEnum = points.some((p) => p.code === c.code && p.options);
        return (
          '<div class="rule-row" data-when="' + i + '">' +
            '<select data-when-device="' + i + '" aria-label="Устройство условия ' + (i + 1) + '">' +
              deviceOptions(c.device, '— устройство —') +
            '</select>' +
            '<select data-when-code="' + i + '" aria-label="Показание условия ' + (i + 1) + '">' +
              pointOptions(c.device, c.code) +
            '</select>' +
            '<select data-when-op="' + i + '" aria-label="Сравнение ' + (i + 1) + '">' +
              ruleHelp.OPS.map(
                (op) =>
                  '<option value="' + op + '"' + (op === c.op ? ' selected' : '') + '>' +
                    esc(ruleHelp.OP_LABELS[op]) +
                  '</option>',
              ).join('') +
            '</select>' +
            '<input data-when-value="' + i + '" type="text" value="' + esc(c.value) + '"' +
              ' placeholder="значение" aria-label="Значение условия ' + (i + 1) + '">' +
            '<button type="button" class="ghost ghost--danger rule-row-x" data-remove-when="' + i + '"' +
              ' aria-label="Убрать условие ' + (i + 1) + '">✕</button>' +
          '</div>'
        );
      })
      .join('');

    const actions = d.actions
      .map((a, i) => {
        const body =
          a.kind === 'scene'
            ? '<select data-action-scene="' + i + '" aria-label="Сцена действия ' + (i + 1) + '">' +
                sceneOptions(a.scene) +
              '</select>'
            : '<select data-action-device="' + i + '" aria-label="Устройство действия ' + (i + 1) + '">' +
                deviceOptions(a.device, '— устройство —') +
              '</select>' +
              '<select data-action-code="' + i + '" aria-label="Функция действия ' + (i + 1) + '">' +
                pointOptions(a.device, a.code) +
              '</select>' +
              '<input data-action-value="' + i + '" type="text" value="' + esc(a.value) + '"' +
                ' placeholder="значение" aria-label="Значение действия ' + (i + 1) + '">';
        return (
          '<div class="rule-row" data-action="' + i + '">' +
            '<select data-action-kind="' + i + '" aria-label="Тип действия ' + (i + 1) + '">' +
              '<option value="command"' + (a.kind === 'command' ? ' selected' : '') + '>устройство</option>' +
              '<option value="scene"' + (a.kind === 'scene' ? ' selected' : '') + '>сцена</option>' +
            '</select>' +
            body +
            '<button type="button" class="ghost ghost--danger rule-row-x" data-remove-action="' + i + '"' +
              ' aria-label="Убрать действие ' + (i + 1) + '">✕</button>' +
          '</div>'
        );
      })
      .join('');

    const days = ruleHelp.DAYS.map(
      (day) =>
        '<label class="rule-day">' +
          '<input type="checkbox" data-day="' + day.code + '"' +
            (d.at.days.indexOf(day.code) >= 0 ? ' checked' : '') + '>' +
          esc(day.label) +
        '</label>',
    ).join('');

    els.ruleBody.innerHTML =
      '<label class="rule-field">Название' +
        '<input id="rule-name" type="text" maxlength="60" value="' + esc(d.name) + '"' +
          ' placeholder="Например: холодно в спальне">' +
      '</label>' +
      '<div class="rule-field rule-field--inline">' +
        '<label>Группа' +
          '<input id="rule-group" type="text" list="rule-group-list" maxlength="60"' +
            ' value="' + esc(d.group) + '" placeholder="без группы">' +
          '<datalist id="rule-group-list">' + groupNames + '</datalist>' +
        '</label>' +
        '<label>Режим' +
          '<select id="rule-mode">' +
            '<option value="edge"' + (d.mode !== 'level' ? ' selected' : '') + '>один раз при переходе</option>' +
            '<option value="level"' + (d.mode === 'level' ? ' selected' : '') + '>повторять, пока верно</option>' +
          '</select>' +
        '</label>' +
      '</div>' +
      '<fieldset class="rule-set">' +
        '<legend>Условия</legend>' +
        '<div id="rule-when">' + conditions + '</div>' +
        '<button type="button" class="ghost rule-add-row" data-add-when="1">＋ условие</button>' +
      '</fieldset>' +
      '<fieldset class="rule-set">' +
        '<legend>Расписание</legend>' +
        '<div class="rule-row">' +
          // clockValue pads a hand-written "7:30": the input would otherwise show
          // empty, and saving would drop the schedule without saying so.
          '<label class="rule-time">с <input type="time" id="rule-from" value="' + esc(ruleHelp.clockValue(d.at.from)) + '"></label>' +
          '<label class="rule-time">по <input type="time" id="rule-to" value="' + esc(ruleHelp.clockValue(d.at.to)) + '"></label>' +
        '</div>' +
        '<div class="rule-days">' + days + '</div>' +
        '<p class="rule-hint">Пусто — без расписания. Заполнено только «с» — сработает в эту минуту.</p>' +
      '</fieldset>' +
      '<div class="rule-field rule-field--inline">' +
        '<label>Держать (for)<input id="rule-for" type="text" value="' + esc(d.for) + '" placeholder="5m"></label>' +
        '<label>Не чаще (cooldown)<input id="rule-cooldown" type="text" value="' + esc(d.cooldown) + '" placeholder="10m"></label>' +
      '</div>' +
      '<fieldset class="rule-set">' +
        '<legend>Действия</legend>' +
        '<div id="rule-actions">' + actions + '</div>' +
        '<button type="button" class="ghost rule-add-row" data-add-action="1">＋ действие</button>' +
      '</fieldset>';
  }

  // readRuleForm copies the DOM into the draft. It runs before any structural
  // re-render, so a value typed into one row is not lost when another row is
  // added or a device is changed.
  function readRuleForm() {
    const d = state.ruleDraft;
    if (!d) return;
    const val = (id) => {
      const el = document.getElementById(id);
      return el ? el.value : '';
    };
    d.name = val('rule-name');
    d.group = val('rule-group');
    d.mode = val('rule-mode') === 'level' ? 'level' : 'edge';
    d.for = val('rule-for');
    d.cooldown = val('rule-cooldown');
    d.at = {
      from: val('rule-from'),
      to: val('rule-to'),
      days: d.at.days.slice(),
    };
    for (const box of els.ruleBody.querySelectorAll('[data-day]')) {
      const day = box.dataset.day;
      const on = box.checked;
      const at = d.at.days.indexOf(day);
      if (on && at < 0) d.at.days.push(day);
      if (!on && at >= 0) d.at.days.splice(at, 1);
    }
    const pick = (root, attr) => {
      const el = els.ruleBody.querySelector('[' + attr + '="' + root + '"]');
      return el ? el.value : '';
    };
    d.when = d.when.map((c, i) => ({
      device: pick(i, 'data-when-device') || c.device,
      code: pick(i, 'data-when-code') || c.code,
      op: pick(i, 'data-when-op') || c.op,
      value: document.querySelector('[data-when-value="' + i + '"]')
        ? document.querySelector('[data-when-value="' + i + '"]').value
        : c.value,
    }));
    d.actions = d.actions.map((a, i) => {
      const kind = pick(i, 'data-action-kind') || a.kind;
      if (kind === 'scene') {
        return { kind: 'scene', scene: pick(i, 'data-action-scene'), device: '', code: '', value: '' };
      }
      const valueEl = document.querySelector('[data-action-value="' + i + '"]');
      return {
        kind: 'command',
        device: pick(i, 'data-action-device') || a.device,
        code: pick(i, 'data-action-code') || a.code,
        value: valueEl ? valueEl.value : a.value,
        scene: '',
      };
    });
  }

  // draftPayload converts the typed strings into the rule the server stores.
  function draftPayload() {
    const d = state.ruleDraft;
    const payload = {
      id: d.id || undefined,
      name: d.name,
      group: d.group,
      enabled: d.enabled,
      mode: d.mode,
      for: d.for,
      cooldown: d.cooldown,
      at: d.at.from ? d.at : null,
      when: d.when
        .filter((c) => c.device && c.code)
        .map((c) => ({ device: c.device, code: c.code, op: c.op, value: ruleHelp.parseValue(c.value) })),
      actions: d.actions
        .filter((a) => (a.kind === 'scene' ? a.scene : a.device && a.code))
        .map((a) =>
          a.kind === 'scene'
            ? { kind: 'scene', scene: a.scene }
            : { kind: 'command', device: a.device, code: a.code, value: ruleHelp.parseValue(a.value) },
        ),
    };
    return payload;
  }

  async function saveRule() {
    const d = state.ruleDraft;
    if (!d) return;
    readRuleForm();
    const payload = draftPayload();
    const errors = ruleHelp.validateDraft(payload);
    if (errors.length) {
      els.ruleErrors.hidden = false;
      els.ruleErrors.textContent = errors.join('; ');
      return;
    }
    const rule = {
      id: payload.id || ruleHelp.makeID(payload.name),
      name: payload.name.trim(),
      enabled: payload.enabled,
      actions: payload.actions,
    };
    if (payload.group) rule.group = payload.group;
    if (payload.when.length) rule.when = payload.when;
    if (payload.at) {
      rule.at = { from: payload.at.from };
      if (payload.at.to) rule.at.to = payload.at.to;
      if (payload.at.days && payload.at.days.length) rule.at.days = payload.at.days.slice();
    }
    if (payload.for) rule.for = payload.for;
    if (payload.cooldown) rule.cooldown = payload.cooldown;
    if (payload.mode === 'level') rule.mode = 'level';

    // One rule is saved as one rule. Sending the whole set would take this page's
    // copy — up to one poll old — and overwrite whatever another screen changed
    // in the meantime, including a group somebody just switched off.
    try {
      const data = await request('/api/rules/' + encodeURIComponent(rule.id), {
        method: 'PUT',
        body: JSON.stringify(rule),
      });
      state.rules = data;
      state.error = '';
      closeRuleEditor();
      render();
    } catch (err) {
      els.ruleErrors.hidden = false;
      els.ruleErrors.textContent = err.message;
    }
  }

  async function switchRule(id, enabled) {
    try {
      state.rules = await request('/api/rules/' + encodeURIComponent(id) + '/enabled', {
        method: 'POST',
        body: JSON.stringify({ enabled }),
      });
      state.error = '';
    } catch (err) {
      state.error = err.message;
    }
    render();
  }

  async function switchGroup(name, enabled) {
    try {
      state.rules = await request('/api/rules/groups/' + encodeURIComponent(name) + '/enabled', {
        method: 'POST',
        body: JSON.stringify({ enabled }),
      });
      state.error = '';
    } catch (err) {
      state.error = err.message;
    }
    render();
  }

  async function deleteRule(id) {
    try {
      state.rules = await request('/api/rules/' + encodeURIComponent(id), { method: 'DELETE' });
      state.error = '';
    } catch (err) {
      state.error = err.message;
    }
    render();
  }

  // checkRules asks the server what would fire right now, without sending
  // anything: waiting for the next tick to learn whether a rule works is a bad
  // way to write one.
  async function checkRules() {
    try {
      const data = await request('/api/rules/check', { method: 'POST', body: '{}' });
      if (data) {
        state.rules = { ...state.rules, ...data, journal: data.journal };
      }
      state.error = '';
    } catch (err) {
      state.error = err.message;
    }
    render();
  }

  // ---------- renaming a device ----------

  // renaming remembers which device the dialog is about. Kept here, not in the
  // DOM, so the eight-second refresh cannot retarget it.
  let renaming = '';

  // cloudName is what the cloud calls the device; localName is what this
  // installation calls it, or '' when nobody has renamed it. The server sends
  // the cloud's name along with a renamed device, because after the rename its
  // `name` is the local one, and "use the cloud's name again" needs the other.
  function cloudName(deviceID) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return deviceID;
    // A renamed device carries both names; an unrenamed one carries only its
    // own. `renamed` decides which case this is, because a device whose cloud
    // name is empty would otherwise look unrenamed and lose its local name on
    // the next save.
    if (device.renamed) return device.cloud_name || deviceID;
    return device.name || deviceID;
  }

  function localName(deviceID) {
    return state.names[deviceID] || '';
  }

  function openRename(deviceID) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return;
    renaming = deviceID;
    const own = localName(deviceID);
    els.renameTitle.textContent = 'Название: ' + (own || device.name || deviceID);
    els.renameInput.value = own;
    els.renameInput.maxLength = state.maxNameLen;
    els.renameInput.placeholder = device.name || deviceID;
    els.renameHint.textContent =
      'Так устройство называется только здесь. В облаке и в приложении Tuya имя не меняется.';
    els.renameReset.hidden = own === '';
    // The settings modal is where this was opened from; leaving it open under
    // the dialog would put two overlays on top of each other. closeSheet, not
    // close(): it forgets the state too, so the next render cannot reopen it.
    if (els.sheet.open) closeSheet();
    if (!els.rename.open && typeof els.rename.showModal === 'function') {
      els.rename.showModal();
    }
    els.renameInput.focus();
    els.renameInput.select();
  }

  function closeRename() {
    const deviceID = renaming;
    renaming = '';
    if (els.rename.open) els.rename.close();
    // Focus goes back to something on the page. Without this it lands on
    // <body>, because the element that opened the dialog (the settings modal)
    // was closed in the meantime — a keyboard user then has to Tab from the top.
    const card = deviceID
      ? Array.from(document.querySelectorAll('[data-more]')).find(
          (el) => el.dataset.more === deviceID && el.closest('#side, #devices'),
        )
      : null;
    if (card) card.focus({ preventScroll: true });
  }

  // saveRename stores the name, then reloads: the server applies names when it
  // serves devices, so the cards, the panel and the chips all pick the new one
  // up from the same answer instead of each formatting it themselves.
  async function saveRename(text) {
    const deviceID = renaming;
    if (!deviceID) return;
    const previous = localName(deviceID);
    const cloud = cloudName(deviceID);
    // Typing the cloud's name back is the same as removing the local one: an
    // override that changes nothing would only make the dialog claim the device
    // is renamed when it is not.
    const next = localNames.normalize(text) === cloud ? '' : localNames.normalize(text);
    if (next === previous) {
      closeRename();
      return;
    }
    if (!state.layoutEditable) {
      // Same rule as the layout: say the change will not survive a restart
      // rather than report a success that a restart undoes.
      state.error = 'Название хранится только в памяти: задайте COOLHOME_DASHBOARD_FILE.';
    }
    const names = Object.assign({}, state.names);
    if (next) names[deviceID] = next;
    else delete names[deviceID];

    // A widget's title is frozen when it is added, so a rename would leave the
    // remote showing the old name beside a card showing the new one.
    const oldTitle = previous || cloud;
    const renamed = localNames.renamedTitles(state.layout, deviceID, oldTitle, next || cloud);
    const layoutChanged = renamed.some((w, i) => w !== state.layout[i]);

    state.names = names;
    if (layoutChanged) state.layout = renamed;
    closeRename();
    render();

    try {
      // One request, not two: the name and the widget titles that follow it are
      // one change, and a failure between two writes would leave the remote
      // showing the old title beside a card showing the new name.
      const body = { names: names };
      if (layoutChanged) body.widgets = state.layout;
      await request('/api/dashboard', {
        method: 'PUT',
        body: JSON.stringify(body),
      });
      state.error = '';
    } catch (err) {
      state.error = 'Не удалось сохранить название: ' + err.message;
    }
    await load({ silent: true });
  }

  // ---------- settings modal ----------

  // openSheet shows one device's settings in a modal. A modal is used instead
  // of a panel inside the card because an absolutely positioned child cannot
  // rise above the sibling cards that follow it in the grid.
  function openSheet(deviceID, code) {
    const device = state.devices.find((d) => d.id === deviceID);
    if (!device) return;
    state.picker = '';
    state.sheet = { deviceID: deviceID, code: code };
    renderSheet();
    if (!els.sheet.open && typeof els.sheet.showModal === 'function') {
      els.sheet.showModal();
    }
  }

  function closeSheet() {
    state.sheet = null;
    state.picker = '';
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
    // The modal repeats the card's figures rather than a single one: opening a
    // sensor must not lose the humidity that was just visible on the wall.
    const drawn = sensorFigures(device, values);
    const figure = drawn.html ? '<div class="sheet-figure">' + drawn.html + '</div>' : '';
    const panel = devicePanel(device, values, drawn.codes, !point);
    els.sheetTitle.textContent = device.name || device.id;
    // Renaming is the one thing here that is not one of the device's functions,
    // so it sits above them, apart.
    const renameButton =
      '<button type="button" class="rename-open" data-rename-open="' + esc(device.id) + '">' +
        '✎ Переименовать<span class="rename-current">' +
          esc(localName(device.id) ? 'сейчас: ' + localName(device.id) : '') +
        '</span>' +
      '</button>';
    els.sheetBody.innerHTML =
      renameButton +
      figure +
      (panel ? '<div class="sheet-controls">' + panel + '</div>' : '<p class="widget-note">Нет настроек.</p>');
  }

  els.activeList.addEventListener('click', (event) => {
    const chip = event.target instanceof Element ? event.target.closest('[data-active]') : null;
    if (!chip) return;
    openSheet(chip.dataset.active, null);
  });

  els.sheetBody.addEventListener('click', (event) => {
    const open = event.target instanceof Element ? event.target.closest('[data-rename-open]') : null;
    if (!open) return;
    openRename(open.dataset.renameOpen);
  });

  els.rename.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const action = event.target.closest('[data-rename]');
    if (action && action.dataset.rename === 'cancel') {
      closeRename();
      return;
    }
    if (action && action.dataset.rename === 'reset') {
      saveRename('');
      return;
    }
    // A click on the backdrop lands on the dialog element itself; closing
    // without saving is what the user means by it.
    if (event.target === els.rename) closeRename();
  });

  els.rename.addEventListener('close', () => {
    renaming = '';
  });

  // Enter saves, Esc closes. The form's default submission would close the
  // dialog without storing anything, so it is intercepted.
  document.getElementById('rename-form').addEventListener('submit', (event) => {
    event.preventDefault();
    saveRename(els.renameInput.value);
  });

  els.renameInput.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeRename();
    }
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
    state.picker = '';
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
    // The panel's picker chooses a device, not a control.
    const pick = event.target.closest('[data-side-pick]');
    if (pick) {
      addToPanel(pick.dataset.sidePick);
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
    const out = valueBeside(input);
    if (!out) return;
    const point = findPoint(input.dataset.id, input.dataset.code);
    if (point) out.textContent = format(Number(input.value), point);
  });

  // ---------- tabs ----------

  function selectTab(name) {
    if (name !== 'devices' && name !== 'scenes' && name !== 'rules') return;
    state.tab = name;
    // Editing lives on the devices tab; leaving it must not leave the grid in a
    // half-edited state.
    if (name !== 'devices' && state.editMode) stopEditing();
    render();
    // Keep the address in step so a reload returns to the same tab.
    const url = new URL(location.href);
    if (name === 'devices') url.searchParams.delete('tab');
    else url.searchParams.set('tab', name);
    history.replaceState(null, '', url);
  }

  for (const btn of els.tabs) {
    btn.addEventListener('click', () => selectTab(btn.dataset.tab));
  }

  // A scene is a button: pressing it runs it. No optimistic state, because
  // there is none to show.
  els.scenes.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const button = event.target.closest('button[data-action="press"]');
    if (!button) return;
    send(button.dataset.id, { code: button.dataset.code, value: true });
  });

  // The scenes tab keeps only its press handler: settings and editing belong to
  // devices, and a scene has no settings.
  els.scenes.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    const card = event.target instanceof Element ? event.target.closest('.card--scene') : null;
    if (!card) return;
    event.preventDefault();
    const button = card.querySelector('button[data-action="press"]');
    if (button) send(button.dataset.id, { code: button.dataset.code, value: true });
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
      openPicker('controls');
      return;
    }
    const index = Number(button.dataset.index);
    applyEdit(action, Number.isInteger(index) ? index : -1, button);
  });

  // The panel's own editing chrome. It is bound to the list element, which
  // survives every repaint, and its buttons carry data-side-edit so they cannot
  // be confused with the remote's data-edit controls.
  els.sideList.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const add = event.target.closest('[data-side-add]');
    if (add) {
      openPicker('panel');
      return;
    }
    const button = event.target.closest('[data-side-edit]');
    if (!button) return;
    applyPanelEdit(button.dataset.sideEdit, button.dataset.side);
  });

  // The side panel sticks below the header, and the header changes height: a
  // wrapped demo badge or a long "обновлено в" pushes it to two rows. Publishing
  // the measured height keeps the panel's title out from under it.
  function publishHeaderHeight() {
    const header = document.querySelector('.topbar');
    if (!header) return;
    document.documentElement.style.setProperty('--header-h', Math.round(header.getBoundingClientRect().height) + 'px');
  }

  publishHeaderHeight();
  if (typeof ResizeObserver === 'function') {
    const header = document.querySelector('.topbar');
    if (header) new ResizeObserver(publishHeaderHeight).observe(header);
  } else {
    window.addEventListener('resize', publishHeaderHeight);
  }

  // ---------- rules events ----------

  els.rulesAdd.addEventListener('click', () => openRuleEditor(null));
  els.rulesCheck.addEventListener('click', checkRules);

  els.rulesList.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const toggle = event.target.closest('[data-rule-toggle]');
    if (toggle) {
      switchRule(toggle.dataset.ruleToggle, toggle.getAttribute('aria-checked') !== 'true');
      return;
    }
    const edit = event.target.closest('[data-rule-edit]');
    if (edit) {
      const rule = (state.rules && state.rules.rules || []).find((r) => r.id === edit.dataset.ruleEdit);
      if (rule) openRuleEditor(rule);
      return;
    }
    const del = event.target.closest('[data-rule-delete]');
    if (del) deleteRule(del.dataset.ruleDelete);
  });

  els.rulesGroups.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const btn = event.target.closest('[data-rule-group]');
    if (!btn) return;
    switchGroup(btn.dataset.ruleGroup, btn.getAttribute('aria-checked') !== 'true');
  });

  els.ruleForm.addEventListener('submit', (event) => {
    // The form's own submission would close the dialog without storing anything.
    event.preventDefault();
    saveRule();
  });

  els.ruleEditor.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    if (event.target.closest('[data-rule="cancel"]') || event.target === els.ruleEditor) {
      closeRuleEditor();
    }
  });

  els.ruleEditor.addEventListener('close', () => {
    state.ruleDraft = null;
  });

  // Typing updates the draft without a repaint: re-rendering on every keystroke
  // would take the caret out of the field being typed into.
  els.ruleBody.addEventListener('input', () => readRuleForm());

  // Structural controls repaint, because they change what other fields offer: a
  // different device has different points, a different action kind has different
  // inputs.
  els.ruleBody.addEventListener('change', (event) => {
    if (!(event.target instanceof Element)) return;
    readRuleForm();
    const t = event.target;
    const structural =
      t.matches('[data-when-device]') ||
      t.matches('[data-action-kind]') ||
      t.matches('[data-action-device]') ||
      t.matches('[data-day]');
    if (structural) renderRuleEditor();
  });

  els.ruleBody.addEventListener('click', (event) => {
    if (!(event.target instanceof Element)) return;
    const addWhen = event.target.closest('[data-add-when]');
    const addAction = event.target.closest('[data-add-action]');
    const remWhen = event.target.closest('[data-remove-when]');
    const remAction = event.target.closest('[data-remove-action]');
    if (!addWhen && !addAction && !remWhen && !remAction) return;
    readRuleForm();
    const d = state.ruleDraft;
    if (!d) return;
    if (addWhen) d.when.push({ device: '', code: '', op: '<', value: '' });
    if (addAction) d.actions.push({ kind: 'command', device: '', code: '', value: '', scene: '' });
    if (remWhen) d.when.splice(Number(remWhen.dataset.removeWhen), 1);
    if (remAction) d.actions.splice(Number(remAction.dataset.removeAction), 1);
    renderRuleEditor();
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
    const params = new URLSearchParams(location.search);
    const fromURL = params.get('tab');
    if (fromURL === 'scenes' || fromURL === 'rules') selectTab(fromURL);
    if (params.get('edit') === '1') startEditing();
  });
})();
