/**
 * Public note visual editor — Semantic UI + inline date wheels.
 */
(function (global) {
  "use strict";

  var state = {
    server: null,
    saveDirect: false,
    formMode: false,
    $modal: null,
    initialized: false,
  };
  var PNE_UNSET_VALUE = "__pne_unset__";

  function formatDate(date) {
    if (!date) return "";
    var y = date.getFullYear();
    var m = ("0" + (date.getMonth() + 1)).slice(-2);
    var d = ("0" + date.getDate()).slice(-2);
    return y + "-" + m + "-" + d;
  }

  function trim(v) {
    return (v == null ? "" : String(v)).trim();
  }

  function parseNote(raw) {
    if (!trim(raw)) return {};
    try {
      var obj = JSON.parse(raw);
      return obj && typeof obj === "object" ? obj : {};
    } catch (e) {
      return {};
    }
  }

  function normalizeDateInput(value) {
    if (!value) return "";
    var s = String(value);
    if (s.indexOf("0000-00-00") === 0) return "lifetime";
    if (s.length >= 10 && s[4] === "-" && s[7] === "-") return s.slice(0, 10);
    var d = new Date(s);
    if (!isNaN(d.getTime())) return formatDate(d);
    return "";
  }

  function readForm() {
    var $m = state.$modal;
    var billingEnabled = $m.find('input[name="pne_billing_enabled"]').is(":checked");
    var planEnabled = $m.find('input[name="pne_plan_enabled"]').is(":checked");
    var countryEnabled = $m.find('input[name="pne_country_enabled"]').is(":checked");

    var billing = {};
    if (billingEnabled) {
      var startDate = trim($m.find('.pne-date-manual[data-target="pne_start_date"]').val());
      var lifetime = $m.find('input[name="pne_lifetime"]').is(":checked");
      var endDate = lifetime ? "0000-00-00" : trim($m.find('.pne-date-manual[data-target="pne_end_date"]').val());
      var cycle = trim($m.find('select[name="pne_cycle"]').val());
      if (cycle === PNE_UNSET_VALUE) cycle = "";
      var amount = trim($m.find('input[name="pne_amount"]').val());

      if (startDate) {
        var normStart = normalizeDateInput(startDate);
        if (normStart && normStart !== "lifetime") billing.startDate = normStart;
        else if (startDate) billing.startDate = startDate;
      }
      if (endDate) {
        if (lifetime) {
          billing.endDate = "0000-00-00";
        } else {
          var normEnd = normalizeDateInput(endDate);
          if (normEnd && normEnd !== "lifetime") billing.endDate = normEnd;
          else if (endDate) billing.endDate = endDate;
        }
      }
      if (cycle) billing.cycle = cycle;
      if (amount !== "") billing.amount = amount;
      if ($m.find('input[name="pne_auto_renewal"]').is(":checked")) billing.autoRenewal = "1";
    }

    var plan = {};
    if (planEnabled) {
      [
        ["bandwidth", "pne_bandwidth"],
        ["trafficVol", "pne_traffic_vol"],
        ["networkRoute", "pne_network_route"],
        ["extra", "pne_extra"],
      ].forEach(function (pair) {
        var val = trim($m.find('input[name="' + pair[1] + '"]').val());
        if (val) plan[pair[0]] = val;
      });
      var trafficType = trim($m.find('select[name="pne_traffic_type"]').val());
      if (trafficType === PNE_UNSET_VALUE) trafficType = "";
      if (trafficType) plan.trafficType = trafficType;
      if ($m.find('input[name="pne_ipv4"]').is(":checked")) plan.IPv4 = "1";
      if ($m.find('input[name="pne_ipv6"]').is(":checked")) plan.IPv6 = "1";
    }

    var out = {};
    if (billingEnabled && Object.keys(billing).length) out.billingDataMod = billing;
    if (planEnabled && Object.keys(plan).length) out.planDataMod = plan;
    if (countryEnabled) {
      var cc = trim($m.find('input[name="pne_country_code"]').val()).toUpperCase();
      if (cc.length === 2) out.countryCode = cc;
    }
    return out;
  }

  function buildJSON() {
    var obj = readForm();
    if (!Object.keys(obj).length) return "";
    return JSON.stringify(obj, null, 2);
  }

  function updatePreview() {
    state.$modal.find('textarea[name="pne_preview"]').val(buildJSON());
  }

  function formatCountryCodeInput(raw) {
    return String(raw || "").replace(/[^A-Za-z]/g, "").toUpperCase().slice(0, 2);
  }

  function bindCountryCodeHandlers() {
    state.$modal
      .find('input[name="pne_country_code"]')
      .off("input.pneCountry blur.pneCountry")
      .on("input.pneCountry blur.pneCountry", function () {
        var $input = $(this);
        var formatted = formatCountryCodeInput($input.val());
        if ($input.val() !== formatted) $input.val(formatted);
        updatePreview();
      });
  }

  var WHEEL_ITEM_H = 40;
  var WHEEL_VIEW_H = 120;
  var WHEEL_YEAR_START = 1999;
  var WHEEL_YEAR_END = 2099;
  // 日列上下各铺 5 组重复的 1..N，配合 scrollColToValue 定位到正中一组，
  // 滚到月末后紧接 1 号，视觉上无限循环
  var WHEEL_DAY_CYCLES = 11;

  function pneMsg(key) {
    return state.$modal.attr("data-pne-" + key) || "";
  }

  function extractDateDigits(raw) {
    return String(raw || "").replace(/\D/g, "").slice(0, 8);
  }

  function formatDateInputDigits(digits) {
    var len = digits.length;
    if (len <= 4) return digits;
    var yyyy = digits.slice(0, 4);
    if (len === 5) return yyyy + "-" + digits.slice(4);
    if (len === 6) {
      var mm = digits.slice(4, 6);
      var mNum = parseInt(mm, 10);
      if (mNum >= 1 && mNum <= 12) return yyyy + "-" + mm;
      if (digits[4] >= "2" && digits[4] <= "9") {
        return yyyy + "-" + digits[4] + "-" + digits[5];
      }
      return yyyy + "-" + mm;
    }
    return yyyy + "-" + digits.slice(4, 6) + "-" + digits.slice(6);
  }

  function addYears(date, years) {
    var d = new Date(date.getTime());
    d.setFullYear(d.getFullYear() + years);
    return d;
  }

  function addOneYear(date) {
    var d = new Date(date.getTime());
    var day = d.getDate();
    d.setDate(1);
    d.setFullYear(d.getFullYear() + 1);
    d.setDate(Math.min(day, daysInMonth(d.getFullYear(), d.getMonth() + 1)));
    return d;
  }

  function defaultWheelDate(target) {
    if (target === "pne_end_date" || target === "EndDate") return addYears(new Date(), 1);
    return new Date();
  }

  function setWheelSuppress($wheel, on) {
    clearTimeout($wheel.data("pneSuppressTimer"));
    $wheel.data("pneSuppressCommit", !!on);
    if (on) return;
    $wheel.data(
      "pneSuppressTimer",
      setTimeout(function () {
        $wheel.data("pneSuppressCommit", false);
      }, 200)
    );
  }

  function positionWheelAt($wheel, y, m, d) {
    initWheelDate($wheel);
    buildWheelLists($wheel, y, m, d);
    setWheelSuppress($wheel, true);
    scrollColToValue($wheel.find('[data-part=year]'), y);
    scrollColToValue($wheel.find('[data-part=month]'), m);
    scrollColToValue($wheel.find('[data-part=day]'), d);
    setWheelSuppress($wheel, false);
    $wheel.data("pneTouched", false);
    $wheel.find(".pne-wheel-col").each(function () {
      updateColVisibility($(this));
    });
  }

  function positionWheelToDate($wheel, isoDate) {
    var parts = isoDate.split("-");
    if (parts.length < 3) return;
    positionWheelAt($wheel, +parts[0], +parts[1], +parts[2]);
  }

  function positionWheelDefault($wheel, date) {
    positionWheelAt($wheel, date.getFullYear(), date.getMonth() + 1, date.getDate());
  }

  function updateDateClearVisibility(target) {
    if (!state.$modal || !state.$modal.length) return;
    var $input = state.$modal.find('.pne-date-manual[data-target="' + target + '"]');
    var $wrap = $input.closest(".pne-wheel-manual");
    var hasValue = !!trim($input.val());
    $wrap.toggleClass("has-value", hasValue);
  }

  function updateAllDateClearVisibility() {
    if (!state.$modal || !state.$modal.length) return;
    state.$modal.find(".pne-date-manual").each(function () {
      updateDateClearVisibility($(this).data("target"));
    });
  }

  function syncInputToWheel($wheel, isoDate) {
    var target = $wheel.data("target");
    if (!isoDate) {
      setWheelEmpty($wheel);
      return;
    }
    state.$modal.find('input[name="' + target + '"]').val(isoDate);
    state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val(isoDate);
    updateDateClearVisibility(target);
    $wheel.data("pneConfigured", true);
    $wheel.data("pneTouched", false);
    positionWheelToDate($wheel, isoDate);
    syncLinkedEndWheel($wheel);
    updatePreview();
  }

  function activateWheel($wheel) {
    $wheel.data("pneTouched", true);
    $wheel.data("pneConfigured", true);
    $wheel.find(".pne-wheel-col").each(function () {
      updateColVisibility($(this));
    });
  }

  function setWheelEmpty($wheel) {
    var target = $wheel.data("target");
    state.$modal.find('input[name="' + target + '"]').val("");
    state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val("");
    updateDateClearVisibility(target);
    $wheel.data("pneTouched", false);
    $wheel.data("pneConfigured", false);
    positionWheelDefault($wheel, defaultWheelDate(target));
    updatePreview();
  }

  function enforceUnconfiguredDateInputs() {
    state.$modal.find(".pne-wheel-date").each(function () {
      var $wheel = $(this);
      if ($wheel.data("pneConfigured")) return;
      var target = $wheel.data("target");
      state.$modal.find('input[name="' + target + '"]').val("");
      state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val("");
    });
    updateAllDateClearVisibility();
  }

  function daysInMonth(y, m) {
    return new Date(y, m, 0).getDate();
  }

  function wheelPad() {
    return (WHEEL_VIEW_H - WHEEL_ITEM_H) / 2;
  }

  function buildDayItems(dim) {
    var html = "";
    for (var c = 0; c < WHEEL_DAY_CYCLES; c++) {
      for (var da = 1; da <= dim; da++) {
        html += '<li data-value="' + da + '">' + ("0" + da).slice(-2) + "</li>";
      }
    }
    return html;
  }

  function buildWheelLists($wheel, y, m, d) {
    var pad = wheelPad();
    var padStyle = "padding:" + pad + "px 0";
    var $year = $wheel.find('[data-part=year] ul').empty().attr("style", padStyle);
    var $month = $wheel.find('[data-part=month] ul').empty().attr("style", padStyle);
    var $day = $wheel.find('[data-part=day] ul').empty().attr("style", padStyle);
    var yr;
    for (yr = WHEEL_YEAR_START; yr <= WHEEL_YEAR_END; yr++) {
      $year.append('<li data-value="' + yr + '">' + yr + "</li>");
    }
    var mo;
    for (mo = 1; mo <= 12; mo++) {
      $month.append('<li data-value="' + mo + '">' + ("0" + mo).slice(-2) + "</li>");
    }
    var dim = daysInMonth(y, m);
    if (d > dim) d = dim;
    $day.append(buildDayItems(dim));
  }

  function scrollColToValue($col, value) {
    var $items = $col.find('li[data-value="' + value + '"]');
    if (!$items.length) return;
    // 循环日列里同一日期出现多组，取正中那组定位；
    // 每次吸附后都回到正中组，向上/向下滚动都始终有富余，实现循环
    var $li = $items.length > 1 ? $items.eq(Math.floor($items.length / 2)) : $items.first();
    $col.scrollTop($li[0].offsetTop - $col[0].clientHeight / 2 + WHEEL_ITEM_H / 2);
  }

  function getColValue($col) {
    var center = $col.scrollTop() + $col[0].clientHeight / 2;
    var best = null;
    var bestDist = Infinity;
    $col.find("li").each(function () {
      var dist = Math.abs(this.offsetTop + WHEEL_ITEM_H / 2 - center);
      if (dist < bestDist) {
        bestDist = dist;
        best = $(this);
      }
    });
    return best ? parseInt(best.data("value"), 10) : null;
  }

  function updateColVisibility($col) {
    var center = $col.scrollTop() + $col[0].clientHeight / 2;
    var val = null;
    $col.find("li").each(function () {
      var itemCenter = this.offsetTop + WHEEL_ITEM_H / 2;
      var steps = Math.round(Math.abs(itemCenter - center) / WHEEL_ITEM_H);
      var $li = $(this);
      $li.removeClass("is-active is-adjacent");
      if (steps === 0) {
        $li.addClass("is-active");
        val = parseInt($li.data("value"), 10);
      } else if (steps === 1) {
        $li.addClass("is-adjacent");
      }
    });
    return val;
  }

  function snapCol($col) {
    var val = getColValue($col);
    if (val != null) scrollColToValue($col, val);
    updateColVisibility($col);
    return val;
  }

  function rebuildWheelDays($wheel, y, m, keepDay) {
    if (!y || !m) return;
    var $dayCol = $wheel.find('[data-part=day]');
    var dim = daysInMonth(y, m);
    var $ul = $dayCol.find("ul");

    // No change in day count: leave the DOM completely untouched so the
    // column never flickers when only the year/day is adjusted.
    if ($ul.children("li").length === dim * WHEEL_DAY_CYCLES) return;

    // 天数变了（含闰年二月）：整列重建为循环列表，选中值回到正中组
    $ul.html(buildDayItems(dim));

    // Only re-scroll when the current selection fell out of range
    // (e.g. 31 -> a 30-day month), otherwise keep the position fixed.
    var target = !keepDay || keepDay > dim ? dim : keepDay;
    scrollColToValue($dayCol, target);
    updateColVisibility($dayCol);
  }

  function stepWheelCol($col, dir) {
    var $wheel = $col.closest(".pne-wheel-date");
    if ($wheel.data("pneSuppressCommit")) return;
    activateWheel($wheel);
    var val = getColValue($col);
    var $ref = val != null ? $col.find('li[data-value="' + val + '"]') : $();
    var $target;
    if ($ref.length) {
      $target = dir > 0 ? $ref.next() : $ref.prev();
    } else {
      $target = dir > 0 ? $col.find("li").first() : $col.find("li").last();
    }
    if (!$target.length) return;
    scrollColToValue($col, $target.data("value"));
    updateColVisibility($col);
    commitWheel($wheel);
  }

  function readWheelIsoDate($wheel) {
    var y = snapCol($wheel.find('[data-part=year]'));
    var m = snapCol($wheel.find('[data-part=month]'));
    var d = getColValue($wheel.find('[data-part=day]'));
    rebuildWheelDays($wheel, y, m, d);
    d = snapCol($wheel.find('[data-part=day]'));
    if (!y || !m || !d) return "";
    return y + "-" + ("0" + m).slice(-2) + "-" + ("0" + d).slice(-2);
  }

  function commitWheel($wheel) {
    if (!$wheel.data("pneTouched")) return "";
    var y = snapCol($wheel.find('[data-part=year]'));
    var m = snapCol($wheel.find('[data-part=month]'));
    var d = getColValue($wheel.find('[data-part=day]'));
    rebuildWheelDays($wheel, y, m, d);
    d = snapCol($wheel.find('[data-part=day]'));
    if (!y || !m || !d) return "";
    var iso = y + "-" + ("0" + m).slice(-2) + "-" + ("0" + d).slice(-2);
    var target = $wheel.data("target");
    state.$modal.find('input[name="' + target + '"]').val(iso);
    state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val(iso);
    updateDateClearVisibility(target);
    updatePreview();
    syncLinkedEndWheel($wheel);
    return iso;
  }

  function wheelDisplayDate($wheel) {
    var y = getColValue($wheel.find('[data-part=year]'));
    var m = getColValue($wheel.find('[data-part=month]'));
    var d = getColValue($wheel.find('[data-part=day]'));
    if (!y || !m || !d) return null;
    return new Date(y, m - 1, d);
  }

  function linkedEndTarget(target) {
    var key = String(target || "").toLowerCase();
    if (key === "pne_start_date") return "pne_end_date";
    if (key === "startdate") return "EndDate";
    return "";
  }

  // 左侧日期改定后，若右侧日期仍为空（未配置），右侧滚轮自动展示左侧日期+1
  // 年；只移动滚轮显示，不写入输入框，右侧一旦配置过或被操作过就不再跟随
  function syncLinkedEndWheel($startWheel) {
    var endTarget = linkedEndTarget($startWheel.data("target"));
    if (!endTarget) return;
    var $end = state.$modal.find('.pne-wheel-date[data-target="' + endTarget + '"]');
    if (!$end.length || $end.data("pneConfigured") || $end.hasClass("is-disabled")) return;
    var base = wheelDisplayDate($startWheel);
    if (!base) return;
    var shifted = addOneYear(base);
    positionWheelAt($end, shifted.getFullYear(), shifted.getMonth() + 1, shifted.getDate());
  }

  function initWheelDate($wheel) {
    if ($wheel.data("pneWheelReady")) return;
    var now = new Date();
    buildWheelLists($wheel, now.getFullYear(), now.getMonth() + 1, now.getDate());

    var snapTimer;
    function afterScroll($col) {
      var $w = $col.closest(".pne-wheel-date");
      clearTimeout(snapTimer);
      snapTimer = setTimeout(function () {
        commitWheel($w);
      }, 80);
    }

    function bindWheelColTouch($col) {
      var colEl = $col[0];
      if (!colEl || $col.data("pneTouchBound")) return;
      $col.data("pneTouchBound", true);

      colEl.addEventListener(
        "touchstart",
        function (e) {
          if (e.touches.length !== 1) return;
          var touch = e.touches[0];
          $col.data("pneTouchY", touch.clientY);
          $col.data("pneTouchScrollTop", colEl.scrollTop);
          $col.data("pneUserDrag", true);
        },
        { passive: true }
      );

      colEl.addEventListener(
        "touchmove",
        function (e) {
          if (e.touches.length !== 1) return;
          var touch = e.touches[0];
          var startY = $col.data("pneTouchY");
          if (startY == null) return;
          var $w = $col.closest(".pne-wheel-date");
          if ($w.data("pneSuppressCommit")) return;
          e.preventDefault();
          colEl.scrollTop = $col.data("pneTouchScrollTop") + (startY - touch.clientY);
          activateWheel($w);
          updateColVisibility($col);
        },
        { passive: false }
      );

      function endTouchDrag() {
        $col.data("pneUserDrag", false);
        $col.data("pneTouchY", null);
        var $w = $col.closest(".pne-wheel-date");
        if ($w.data("pneSuppressCommit")) return;
        snapCol($col);
        commitWheel($w);
      }

      colEl.addEventListener("touchend", endTouchDrag, { passive: true });
      colEl.addEventListener("touchcancel", endTouchDrag, { passive: true });
    }

    $wheel.find(".pne-wheel-col").each(function () {
      var $col = $(this);
      bindWheelColTouch($col);

      $col
      .on("mousedown.pneWheel", function () {
        $(this).data("pneUserDrag", true);
      })
      .on("mouseup.pneWheel mouseleave.pneWheel", function () {
        $(this).data("pneUserDrag", false);
      })
      .on("scroll.pneWheel", function () {
        var $col = $(this);
        var $w = $col.closest(".pne-wheel-date");
        if ($w.data("pneSuppressCommit")) return;
        updateColVisibility($col);
        if (!$w.data("pneTouched")) {
          if ($col.data("pneUserDrag")) activateWheel($w);
          else return;
        }
        afterScroll($col);
      })
      .on("wheel.pneWheel", function (e) {
        e.preventDefault();
        var $col = $(this);
        var now = Date.now();
        var last = $col.data("pneWheelAt") || 0;
        if (now - last < 100) return;
        $col.data("pneWheelAt", now);
        var dir = e.originalEvent.deltaY > 0 ? 1 : -1;
        stepWheelCol($col, dir);
      })
      .on("click.pneWheel", "li", function () {
        var $col = $(this).closest(".pne-wheel-col");
        var $w = $col.closest(".pne-wheel-date");
        activateWheel($w);
        scrollColToValue($col, $(this).data("value"));
        commitWheel($w);
      });
    });

    $wheel.data("pneWheelReady", true);
  }

  function bindManualDateHandlers() {
    state.$modal
      .find(".pne-date-manual")
      .off("focus.pneManual input.pneManual blur.pneManual")
      .on("focus.pneManual", function () {
        // 不再在点击日期输入框时自动从滚轮填充日期
      })
      .on("input.pneManual", function () {
        var $input = $(this);
        // Defer reformatting to the next frame so the browser finishes
        // applying its own character insertion first. Mutating value()
        // synchronously inside the input event makes some IMEs/keyboards
        // re-insert the keystroke, doubling the digit (e.g. 7 -> 77).
        if ($input.data("pneFmtScheduled")) return;
        $input.data("pneFmtScheduled", true);
        var raf = global.requestAnimationFrame || function (fn) { return setTimeout(fn, 0); };
        raf(function () {
          $input.data("pneFmtScheduled", false);
          var formatted = formatDateInputDigits(extractDateDigits($input.val()));
          if ($input.val() !== formatted) {
            $input.val(formatted);
            var el = $input[0];
            if (el && el.setSelectionRange) {
              try {
                el.setSelectionRange(formatted.length, formatted.length);
              } catch (e) {}
            }
          }
          updateDateClearVisibility($input.data("target"));
        });
      })
      .on("blur.pneManual", function () {
        var $input = $(this);
        var target = $input.data("target");
        var $wheel = state.$modal.find('.pne-wheel-date[data-target="' + target + '"]');
        var raw = trim($input.val());
        if (!raw) {
          state.$modal.find('input[name="' + target + '"]').val("");
          updateDateClearVisibility(target);
          updatePreview();
          return;
        }
        updateDateClearVisibility(target);
        $wheel.data("pneConfigured", true);
        var norm = normalizeDateInput(raw);
        if (norm && norm !== "lifetime") {
          $input.val(norm);
          syncInputToWheel($wheel, norm);
        }
        updatePreview();
      });

    state.$modal
      .find(".pne-wheel-clear")
      .off("click.pneClear")
      .on("click.pneClear", function (e) {
        e.preventDefault();
        var target = $(this).data("target");
        setWheelEmpty(state.$modal.find('.pne-wheel-date[data-target="' + target + '"]'));
      });
  }

  function initAllWheelDates() {
    state.$modal.find(".pne-wheel-date").each(function () {
      var $wheel = $(this);
      var target = $wheel.data("target");
      var val = trim(state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val());
      state.$modal.find('input[name="' + target + '"]').val(val);
      if (val) {
        var norm = normalizeDateInput(val);
        if (norm && norm !== "lifetime") {
          $wheel.data("pneConfigured", true);
          $wheel.data("pneTouched", false);
          positionWheelToDate($wheel, norm);
          return;
        }
      }
      state.$modal.find('input[name="' + target + '"]').val("");
      state.$modal.find('.pne-date-manual[data-target="' + target + '"]').val("");
      $wheel.data("pneConfigured", false);
      $wheel.data("pneTouched", false);
      positionWheelDefault($wheel, defaultWheelDate(target));
    });
    updateAllDateClearVisibility();
    bindManualDateHandlers();
  }

  function syncEndDateWheel() {
    var lifetime = state.$modal.find('input[name="pne_lifetime"]').is(":checked");
    var $end = state.$modal.find('.pne-wheel-date[data-target="pne_end_date"]');
    $end.toggleClass("is-disabled", lifetime);
    if (lifetime) {
      state.$modal.find('input[name="pne_end_date"]').val("");
      state.$modal.find('.pne-date-manual[data-target="pne_end_date"]').val("");
      $end.data("pneConfigured", false);
      $end.data("pneTouched", false);
      setWheelEmpty($end);
    }
  }

  function toggleTabBody(tab, enabled) {
    state.$modal.find('.pne-tab-body[data-tab="' + tab + '"]').toggleClass("is-disabled", !enabled);
  }

  function setCheckbox(name, checked) {
    var $box = state.$modal.find('input[name="' + name + '"]').closest(".ui.checkbox");
    $box.checkbox(checked ? "set checked" : "set unchecked");
  }

  function fillFormFromNote(note) {
    var $m = state.$modal;
    var billing = note.billingDataMod || {};
    var plan = note.planDataMod || {};
    var hasBilling = !!Object.keys(billing).length;
    var hasPlan = !!Object.keys(plan).length;
    var hasCountry = !!trim(note.countryCode);

    setCheckbox("pne_billing_enabled", hasBilling || !note || !Object.keys(note).length);
    setCheckbox("pne_plan_enabled", hasPlan);
    setCheckbox("pne_country_enabled", hasCountry);
    toggleTabBody("pne-billing", hasBilling || !note || !Object.keys(note).length);
    toggleTabBody("pne-plan", hasPlan);
    toggleTabBody("pne-country", hasCountry);

    var startNorm = normalizeDateInput(billing.startDate);
    var startVal = startNorm === "lifetime" ? "" : startNorm;
    $m.find('input[name="pne_start_date"]').val(startVal);
    $m.find('.pne-date-manual[data-target="pne_start_date"]').val(startVal);

    var endNorm = normalizeDateInput(billing.endDate);
    if (endNorm === "lifetime") {
      setCheckbox("pne_lifetime", true);
      $m.find('input[name="pne_end_date"]').val("");
      $m.find('.pne-date-manual[data-target="pne_end_date"]').val("");
    } else {
      setCheckbox("pne_lifetime", false);
      var endVal = endNorm || "";
      $m.find('input[name="pne_end_date"]').val(endVal);
      $m.find('.pne-date-manual[data-target="pne_end_date"]').val(endVal);
    }

    var $cycle = $m.find('select[name="pne_cycle"]');
    if (billing.cycle) {
      $cycle.dropdown("set selected", billing.cycle);
    } else {
      $cycle.dropdown("set selected", PNE_UNSET_VALUE);
    }
    $m.find('input[name="pne_amount"]').val(billing.amount != null ? billing.amount : "");
    setCheckbox("pne_auto_renewal", String(billing.autoRenewal) === "1");

    $m.find('input[name="pne_bandwidth"]').val(plan.bandwidth || "");
    $m.find('input[name="pne_traffic_vol"]').val(plan.trafficVol || "");
    var $trafficType = $m.find('select[name="pne_traffic_type"]');
    if (plan.trafficType) {
      $trafficType.dropdown("set selected", String(plan.trafficType));
    } else {
      $trafficType.dropdown("set selected", PNE_UNSET_VALUE);
    }
    setCheckbox("pne_ipv4", String(plan.IPv4) === "1");
    setCheckbox("pne_ipv6", String(plan.IPv6) === "1");
    $m.find('input[name="pne_network_route"]').val(plan.networkRoute || "");
    $m.find('input[name="pne_extra"]').val(plan.extra || "");

    $m.find('input[name="pne_country_code"]').val(formatCountryCodeInput(note.countryCode || ""));
    updateAllDateClearVisibility();
    updatePreview();
  }

  function ensureModal() {
    if (state.initialized) return;

    state.$modal = $(".pne-modal");
    if (!state.$modal.length) return;

    state.$modal.find(".menu .item").tab();
    state.$modal.find(".ui.dropdown").each(function () {
      var $dropdown = $(this);
      if ($dropdown.attr("name") === "pne_cycle" || $dropdown.attr("name") === "pne_traffic_type") {
        $dropdown.dropdown({ forceSelection: false });
      } else {
        $dropdown.dropdown();
      }
    });
    state.$modal.find(".ui.checkbox").checkbox();
    bindCountryCodeHandlers();

    state.$modal.on("input change", "input, select, textarea", function () {
      updatePreview();
    });

    state.$modal.on("change", 'input[name="pne_billing_enabled"]', function () {
      toggleTabBody("pne-billing", $(this).is(":checked"));
      updatePreview();
    });
    state.$modal.on("change", 'input[name="pne_plan_enabled"]', function () {
      toggleTabBody("pne-plan", $(this).is(":checked"));
      updatePreview();
    });
    state.$modal.on("change", 'input[name="pne_country_enabled"]', function () {
      toggleTabBody("pne-country", $(this).is(":checked"));
      updatePreview();
    });
    state.$modal.on("change", 'input[name="pne_lifetime"]', function () {
      syncEndDateWheel();
      updatePreview();
    });

    toggleTabBody("pne-plan", false);

    state.$modal.find(".pne-apply-btn").on("click", function () {
      applyToTextarea();
      closeModal();
      $.suiAlert({
        title: pneMsg("applied-title"),
        description: pneMsg("applied-desc"),
        type: "success",
        time: "2",
        position: "top-center",
      });
    });

    state.$modal.find(".pne-save-btn").on("click", function () {
      saveAndApply($(this));
      return false;
    });

    if (!state.$modal.data("modal-initialized")) {
      state.$modal.modal({
        closable: false,
        detachable: true,
        observeChanges: true,
        keyboard: true,
        transition: "scale",
        duration: 300,
        onShow: function () {
          $(document).on("keydown.pneModalEsc", function (e) {
            if ((e.key === "Escape" || e.keyCode === 27) && state.$modal.hasClass("active")) {
              state.$modal.find(".deny.button").first().trigger("click");
            }
          });

          setTimeout(function () {
            var $dimmer = state.$modal.closest(".ui.dimmer");
            var mouseDownOnDimmer = false;

            $dimmer.off("mousedown.pneModalClickClose mouseup.pneModalClickClose");
            $dimmer.on("mousedown.pneModalClickClose", function (e) {
              if ($(e.target).hasClass("dimmer")) mouseDownOnDimmer = true;
            });
            $dimmer.on("mouseup.pneModalClickClose", function (e) {
              if (mouseDownOnDimmer && $(e.target).hasClass("dimmer")) {
                if (!state.$modal.hasClass("animating")) {
                  state.$modal.find(".deny.button").first().trigger("click");
                }
              }
              mouseDownOnDimmer = false;
            });
          }, 0);
        },
        onHidden: function () {
          $(document).off("keydown.pneModalEsc");
          state.$modal.closest(".ui.dimmer").off("mousedown.pneModalClickClose mouseup.pneModalClickClose");
        },
        onVisible: function () {
          initAllWheelDates();
          syncEndDateWheel();
          state.$modal.modal("refresh");
          // 锁定内容区高度到账单 tab 的高度，切 tab 不再跳动
          requestAnimationFrame(function () {
            var $content = state.$modal.find('.content');
            $content.css('min-height', $content.outerHeight() + 'px');
          });
        },
        onApprove: function () {
          return false;
        },
      });
      state.$modal.data("modal-initialized", true);
    }

    state.initialized = true;
  }

  function applyToTextarea() {
    enforceUnconfiguredDateInputs();
    $(".server.modal textarea[name=PublicNote]").val(buildJSON());
  }

  function serverToPayload(server, publicNote) {
    return {
      id: server.ID,
      name: server.Name,
      Tag: server.Tag || "",
      DisplayIndex: server.DisplayIndex != null ? server.DisplayIndex : 0,
      secret: server.Secret || "",
      Note: server.Note || "",
      PublicNote: publicNote,
      EnableDDNS: server.EnableDDNS ? "on" : "",
      HideForGuest: server.HideForGuest ? "on" : "",
      DDNSProfilesRaw: server.DDNSProfilesRaw || "[]",
    };
  }

  function saveAndApply($btn) {
    if ($btn.hasClass("loading")) return false;
    enforceUnconfiguredDateInputs();
    var json = buildJSON();

    if (state.formMode && !state.saveDirect) {
      applyToTextarea();
      closeModal();
      return false;
    }

    if (!state.server || !state.server.ID) {
      applyToTextarea();
      closeModal();
      $.suiAlert({
        title: pneMsg("written-title"),
        description: pneMsg("written-desc"),
        type: "info",
        time: "3",
        position: "top-center",
      });
      return false;
    }

    $btn.addClass("loading");
    $.post("/api/server", JSON.stringify(serverToPayload(state.server, json)))
      .done(function (resp) {
        $btn.removeClass("loading");
        if (resp.code === 200) {
          closeModal();
          window.location.reload();
        } else {
          $.suiAlert({
            title: pneMsg("save-failed"),
            description: resp.message || pneMsg("unknown-error"),
            type: "error",
            time: "3",
            position: "top-center",
          });
        }
      })
      .fail(function (err) {
        $btn.removeClass("loading");
        $.suiAlert({
          title: pneMsg("save-failed"),
          description: String(err),
          type: "error",
          time: "3",
          position: "top-center",
        });
      });
    return false;
  }

  function closeModal() {
    state.$modal.modal("hide");
  }

  function open(options) {
    options = options || {};
    ensureModal();
    state.server = options.server || null;
    state.saveDirect = !!options.saveDirect;
    state.formMode = !!options.formMode;

    var raw = options.publicNote;
    if (raw == null && state.server) raw = state.server.PublicNote;
    if (raw == null && state.formMode) {
      raw = $(".server.modal textarea[name=PublicNote]").val();
    }
    fillFormFromNote(parseNote(raw));

    state.$modal.find(".pne-apply-btn").toggle(!state.saveDirect);
    state.$modal.modal("show");
  }

  function initDateWheels(modal) {
    state.$modal = $(modal);
    if (!state.$modal.length) return;
    initAllWheelDates();
  }

  global.PublicNoteEditor = { open: open };
  global.NezhaDateWheel = { init: initDateWheels };
})(window);
