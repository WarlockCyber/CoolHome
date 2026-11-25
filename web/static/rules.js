// Rule form helpers: what a rule looks like as a sentence, what the editor sends,
// and what makes a draft invalid.
//
// Kept out of app.js because the mistakes here are quiet ones: a value typed as
// "22" saved as the string instead of the number, a rule with no trigger that
// silently never runs, an action that names a device which is no longer there.
(function (root) {
  'use strict';

  var OPS = ['<', '<=', '>', '>=', '==', '!='];

  var OP_LABELS = {
    '<': 'меньше',
    '<=': 'не больше',
    '>': 'больше',
    '>=': 'не меньше',
    '==': 'равно',
    '!=': 'не равно',
  };

  var DAYS = [
    { code: 'mon', label: 'Пн' },
    { code: 'tue', label: 'Вт' },
    { code: 'wed', label: 'Ср' },
    { code: 'thu', label: 'Чт' },
    { code: 'fri', label: 'Пт' },
    { code: 'sat', label: 'Сб' },
    { code: 'sun', label: 'Вс' },
  ];

  // parseValue turns what a person typed into the value the server compares
  // with. A number stays a number — "22" as text would compare as text against a
  // numeric reading and quietly never match.
  function parseValue(raw) {
    var text = String(raw == null ? '' : raw).trim();
    if (text === '') return null;
    // A Russian keyboard writes 18,5 — the decimal comma is what the person
    // means, and keeping it as text made a numeric comparison that could never
    // match. A comma is only read as a decimal point when the rest is a number,
    // so "а,б" stays text.
    if (/^-?\d+,\d+$/.test(text)) return Number(text.replace(',', '.'));
    if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
    if (text === 'true') return true;
    if (text === 'false') return false;
    return text;
  }

  // formatValue is the inverse for display: numbers plain, booleans as words.
  function formatValue(v) {
    if (v === true) return 'вкл';
    if (v === false) return 'выкл';
    return String(v == null ? '' : v);
  }

  // isZeroDuration recognises the spellings of "no wait": an empty string, a
  // bare 0, and 0s/0m/0h.
  // clockValue normalises a time for <input type="time">, which only accepts
  // zero-padded 24-hour "HH:MM". A hand-written "7:30" is a valid rule but an
  // invalid field value: the input shows empty, and saving then quietly drops
  // the schedule.
  function clockValue(raw) {
    var text = String(raw == null ? '' : raw).trim();
    if (!text) return '';
    var m = /^(\d{1,2}):(\d{2})(?::(\d{2}))?$/.exec(text);
    if (!m) return '';
    var hh = m[1].length === 1 ? '0' + m[1] : m[1];
    return hh + ':' + m[2];
  }

  function isZeroDuration(raw) {
    var text = String(raw == null ? '' : raw).trim();
    if (text === '' || text === '0') return true;
    return /^0+(s|m|h|ms)$/.test(text);
  }

  function dayLabel(code) {
    for (var i = 0; i < DAYS.length; i++) {
      if (DAYS[i].code === code) return DAYS[i].label;
    }
    return code;
  }

  // scheduleText renders a window as a sentence. An empty "to" is a one-minute
  // window, which is how "at 7:30" is expressed.
  function scheduleText(at) {
    if (!at || !at.from) return '';
    var when = at.to ? at.from + '–' + at.to : 'в ' + at.from;
    var days = '';
    if (at.days && at.days.length && at.days.length < 7) {
      var labels = [];
      for (var i = 0; i < at.days.length; i++) labels.push(dayLabel(at.days[i]));
      days = labels.join(', ') + ' ';
    } else if (!at.days || at.days.length === 0) {
      days = 'каждый день ';
    }
    return days + when;
  }

  // conditionText names one comparison. The device label comes from the caller
  // because only the page knows the device names.
  function conditionText(cond, labelOf) {
    var name = labelOf ? labelOf(cond.device, cond.code) : cond.code;
    return name + ' ' + (OP_LABELS[cond.op] || cond.op) + ' ' + formatValue(cond.value);
  }

  function actionText(action, labelOf) {
    if (action.kind === 'scene') {
      return 'сцена ' + (labelOf ? labelOf(action.scene, '') : action.scene);
    }
    var name = labelOf ? labelOf(action.device, action.code) : action.device + '.' + action.code;
    return name + ' → ' + formatValue(action.value);
  }

  // summarize turns a rule into the lines the list shows.
  function summarize(rule, labelOf) {
    var when = [];
    for (var i = 0; i < (rule.when || []).length; i++) {
      when.push(conditionText(rule.when[i], labelOf));
    }
    var schedule = scheduleText(rule.at);
    var trigger = [];
    if (schedule) trigger.push(schedule);
    if (when.length) trigger.push('если ' + when.join(' и '));

    var actions = [];
    for (var j = 0; j < (rule.actions || []).length; j++) {
      actions.push(actionText(rule.actions[j], labelOf));
    }

    var notes = [];
    // "0s" and "0" are how a zero arrives from the editor; saying "держать 0s"
    // is noise, and the rule without a hold is the normal case.
    if (rule.for && !isZeroDuration(rule.for)) notes.push('держать ' + rule.for);
    if (rule.cooldown && !isZeroDuration(rule.cooldown)) notes.push('не чаще ' + rule.cooldown);
    if (rule.mode === 'level') notes.push('повторять, пока верно');

    return {
      trigger: trigger.join(' '),
      actions: actions.join(', '),
      notes: notes.join(', '),
    };
  }

  // validateDraft reports what stops a draft from being saved. It mirrors the
  // server's rules, so the person sees the problem next to the field instead of
  // as a 400 after pressing save.
  function validateDraft(draft) {
    var errors = [];
    if (!draft || !String(draft.name || '').trim()) errors.push('нужно название');
    var when = (draft && draft.when) || [];
    var actions = (draft && draft.actions) || [];
    if (!when.length && !(draft && draft.at && draft.at.from)) {
      errors.push('нужно условие или расписание — иначе правило не сработает никогда');
    }
    for (var i = 0; i < when.length; i++) {
      if (!when[i].device || !when[i].code) errors.push('в условии ' + (i + 1) + ' не выбрано показание');
      if (when[i].value === null || when[i].value === undefined || when[i].value === '') {
        errors.push('в условии ' + (i + 1) + ' нет значения');
      }
    }
    if (!actions.length) errors.push('нужно хотя бы одно действие');
    for (var j = 0; j < actions.length; j++) {
      if (actions[j].kind === 'scene') {
        if (!actions[j].scene) errors.push('в действии ' + (j + 1) + ' не выбрана сцена');
      } else if (!actions[j].device || !actions[j].code) {
        errors.push('в действии ' + (j + 1) + ' не выбрано устройство');
      }
    }
    return errors;
  }

  // toPayload turns a draft into the rule the server stores: empty optional parts
  // are omitted rather than sent as empty strings, which the server would accept
  // but a human reading the file would not understand.
  function toPayload(draft, id) {
    var rule = {
      id: id || draft.id || makeID(draft.name),
      name: String(draft.name || '').trim(),
      enabled: draft.enabled !== false,
      actions: (draft.actions || []).map(function (a) {
        if (a.kind === 'scene') return { kind: 'scene', scene: a.scene };
        return { kind: 'command', device: a.device, code: a.code, value: a.value };
      }),
    };
    if (draft.group) rule.group = draft.group;
    if ((draft.when || []).length) {
      rule.when = draft.when.map(function (c) {
        return { device: c.device, code: c.code, op: c.op, value: c.value };
      });
    }
    if (draft.at && draft.at.from) {
      rule.at = { from: draft.at.from };
      if (draft.at.to) rule.at.to = draft.at.to;
      if (draft.at.days && draft.at.days.length) rule.at.days = draft.at.days.slice();
      if (!rule.at.days) delete rule.at.days;
    }
    if (draft.for) rule.for = String(draft.for).trim();
    if (draft.cooldown) rule.cooldown = String(draft.cooldown).trim();
    if (draft.mode === 'level') rule.mode = 'level';
    return rule;
  }

  // makeID derives a stable id from the name. Cyrillic is transliterated rather
  // than dropped, so "Холодно в спальне" becomes a readable id instead of an
  // empty string.
  var TRANSLIT = {
    а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z',
    и: 'i', й: 'y', к: 'k', л: 'l', м: 'm', н: 'n', о: 'o', п: 'p', р: 'r',
    с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'c', ч: 'ch', ш: 'sh', щ: 'sch',
    ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
  };

  // seq makes ids unique within one page. The time suffix alone is not enough:
  // two rules created in the same millisecond, or two calls in a test, produced
  // the same id — and a duplicate id is rejected by the server after the person
  // has already typed everything.
  var seq = 0;

  function makeID(name) {
    var text = String(name || '').toLowerCase().trim();
    var out = '';
    for (var i = 0; i < text.length; i++) {
      var ch = text[i];
      out += Object.prototype.hasOwnProperty.call(TRANSLIT, ch) ? TRANSLIT[ch] : ch;
    }
    out = out.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    if (!out) out = 'rule';
    // A suffix keeps two rules with the same name from colliding when the second
    // one is created: the counter is what makes it certain, the clock only keeps
    // it readable.
    seq = (seq + 1) % 1296;
    var suffix = Date.now().toString(36).slice(-3) + seq.toString(36).padStart(2, '0');
    return out.slice(0, 36) + '-' + suffix;
  }

  var api = {
    OPS: OPS,
    OP_LABELS: OP_LABELS,
    DAYS: DAYS,
    parseValue: parseValue,
    formatValue: formatValue,
    isZeroDuration: isZeroDuration,
    clockValue: clockValue,
    scheduleText: scheduleText,
    conditionText: conditionText,
    actionText: actionText,
    summarize: summarize,
    validateDraft: validateDraft,
    toPayload: toPayload,
    makeID: makeID,
  };

  root.CoolHomeRules = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
