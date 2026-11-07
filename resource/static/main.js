function initFormSelect(select, settings) {
  if (select.is('select.dashboard-native-select')) {
    return select;
  }
  select.dropdown(settings);
  return select.closest('.ui.dropdown');
}

function setFormSelectValue(select, value) {
  if (select.is('select.dashboard-native-select')) {
    select.val(value).trigger('change');
  } else {
    select.dropdown('set selected', value);
  }
}

// CSRF 保护：从 Cookie 读取 CSRF token
function getCSRFToken() {
  var match = document.cookie.match(new RegExp('(^| )nz-csrf=([^;]+)'));
  return match ? decodeURIComponent(match[2]) : '';
}

// 全局 AJAX 设置：自动为非安全方法附加 CSRF token
$.ajaxSetup({
  beforeSend: function(xhr, settings) {
    if (!/^(GET|HEAD|OPTIONS|TRACE)$/i.test(settings.type)) {
      var csrfToken = getCSRFToken();
      if (csrfToken) {
        xhr.setRequestHeader('X-CSRF-Token', csrfToken);
      }
    }
  }
});

let LANG = {
  Add: "添加",
  Edit: "修改",
  AlarmRule: "报警规则",
  Notification: "通知方式",
  Server: "服务器",
  Monitor: "监控",
  Cron: "计划任务",
}

function updateLang(newLang) {
  if (newLang) {
    LANG = newLang;
  }
}

function readableBytes(bytes) {
  if (!bytes) {
    return '0B'
  }
  var i = Math.floor(Math.log(bytes) / Math.log(1024)),
    sizes = ["B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];
  return parseFloat((bytes / Math.pow(1024, i)).toFixed(2)) + sizes[i];
}

const confirmBtn = $(".mini.confirm.modal .nezha-primary-btn.button");

function showConfirm(title, content, callFn, extData) {
  const modal = $(".mini.confirm.modal");
  modal.children(".header").text(title);
  modal.children(".content").text(content);
  if (confirmBtn.hasClass("loading")) {
    return false;
  }
  modal
    .modal({
      closable: true,
      onApprove: function () {
        confirmBtn.toggleClass("loading");
        callFn(extData);
        return false;
      },
    })
    .modal("show");
}

function postJson(url, data) {
  return $.ajax({
    url: url,
    type: "POST",
    contentType: "application/json",
    data: JSON.stringify(data),
  }).done((resp) => {
    if (resp.code == 200) {
      if (resp.message) {
        alert(resp.message);
      } else {
        alert("删除成功");
      }
      window.location.reload();
    } else {
      alert("删除失败 " + resp.code + "：" + resp.message);
      confirmBtn.toggleClass("loading");
    }
  })
    .fail((err) => {
      alert("网络错误：" + err.responseText);
    });
}

function showFormModal(modelSelector, formID, URL, getData, onSuccess, onVisible) {
  const $modal = $(modelSelector);
  $modal.data("modal-url", URL);
  $modal.data("modal-form-id", formID);
  $modal.data("modal-get-data", getData || null);
  $modal.data("modal-on-success", onSuccess || null);
  $modal.data("modal-on-visible", onVisible || null);

  // 初始化 modal 一次
  if (!$modal.data("modal-initialized")) {
    if ($modal.find(".modal-esc-deny").length === 0) {
      $modal.append(
        `<div class="actions" style="display:none">
           <button class="ui deny button modal-esc-deny"></button>
         </div>`
      );
    }

    $modal.modal({
      closable: false,
      detachable: true,
      observeChanges: true,
      keyboard: true,
      transition: "scale",
      duration: 300,
      autofocus: !$modal.hasClass("notification") && !$modal.hasClass("rule"),

      onShow: function () {
        $(document).on("keydown.modalEsc", function (e) {
          if ((e.key === "Escape" || e.keyCode === 27) && $modal.hasClass("active")) {
            $modal.find(".modal-esc-deny").trigger("click");
          }
        });

        // 点击遮罩严格关闭
        setTimeout(() => {
          const $dimmer = $modal.closest(".ui.dimmer");
          let mouseDownOnDimmer = false;

          $dimmer.off("mousedown.modalClickClose mouseup.modalClickClose");
          $dimmer.on("mousedown.modalClickClose", function (e) {
            if ($(e.target).hasClass("dimmer")) mouseDownOnDimmer = true;
          });
          $dimmer.on("mouseup.modalClickClose", function (e) {
            if (mouseDownOnDimmer && $(e.target).hasClass("dimmer")) {
              // 隐藏弹窗不能触发 deny，否则关闭完成回调不会执行。
              if ($modal.hasClass("active") && !$modal.hasClass("animating")) {
                $modal.find(".modal-esc-deny").trigger("click");
              }
            }
            mouseDownOnDimmer = false;
          });
        }, 0);
      },

      onHidden: function () {
        $(document).off("keydown.modalEsc");
        $modal.closest(".ui.dimmer").off("mousedown.modalClickClose mouseup.modalClickClose");
      },

      onVisible: function () {
        const visible = $modal.data("modal-on-visible");
        if (visible) visible();
      },

      onApprove: function () {
        let success = false;
        const btn = $(modelSelector + " .nezha-primary-btn.button");
        const form = $(modelSelector + " form");
        if (btn.hasClass("loading")) {
          return success;
        }
        form.children(".message").remove();
        btn.toggleClass("loading");
        const readForm = $modal.data("modal-get-data");
        const formSelector = $modal.data("modal-form-id");
        const data = readForm
          ? readForm()
          : $(formSelector)
            .serializeArray()
            .reduce(function (obj, item) {
              // ID 类的数据
              if (
                item.name.endsWith("_id") ||
                item.name === "id" ||
                item.name === "ID" ||
                item.name === "ServerID" ||
                item.name === "RequestType" ||
                item.name === "RequestMethod" ||
                item.name === "SMTPPort" ||
                item.name === "TriggerMode" ||
                item.name === "TaskType" ||
                item.name === "DisplayIndex" ||
                item.name === "Type" ||
                item.name === "Cover" ||
                item.name === "Duration" ||
                item.name === "MaxRetries" ||
                item.name === "Provider" ||
                item.name === "WebhookMethod" ||
                item.name === "WebhookRequestType"
              ) {
                obj[item.name] = parseInt(item.value);
              } else if (item.name.endsWith("Latency")) {
                obj[item.name] = parseFloat(item.value);
              } else if (item.name === "Command") {
                obj[item.name] = trimCommandTrailingBlankLines(item.value);
              } else {
                obj[item.name] = item.value;
              }

              if (item.name.endsWith("ServersRaw")) {
                if (item.value.length > 2) {
                  obj[item.name] = JSON.stringify(
                    [...item.value.matchAll(/\d+/gm)].map((k) =>
                      parseInt(k[0])
                    )
                  );
                }
              }

              if (item.name.endsWith("TasksRaw")) {
                if (item.value.length > 2) {
                  obj[item.name] = JSON.stringify(
                    [...item.value.matchAll(/\d+/gm)].map((k) =>
                      parseInt(k[0])
                    )
                  );
                }
              }

              if (item.name.endsWith("DDNSProfilesRaw")) {
                if (item.value.length > 2) {
                  obj[item.name] = JSON.stringify(
                    [...item.value.matchAll(/\d+/gm)].map((k) =>
                      parseInt(k[0])
                    )
                  );
                }
              }

              return obj;
            }, {});
        $.post($modal.data("modal-url"), JSON.stringify(data))
          .done(function (resp) {
            if (resp.code == 200) {
              const successCb = $modal.data("modal-on-success");
              if (successCb) {
                successCb(resp);
              } else {
                window.location.reload()
              }
            } else {
              form.append(
                $('<div class="ui negative message"><div class="header">操作失败</div></div>')
                  .append($("<p>").text(resp.message))
              );
            }
          })
          .fail(function (err) {
            form.append(
              $('<div class="ui negative message"><div class="header">网络错误</div></div>')
                .append($("<p>").text(err.responseText))
            );
          })
          .always(function () {
            btn.toggleClass("loading");
          });
        return success;
      },
    });

    $modal.data("modal-initialized", true);
  }

  // 防止动画中重复 show
  if (!$modal.hasClass("active") && !$modal.hasClass("animating")) {
    $modal.modal("show");
  }
}

function trimCommandTrailingBlankLines(command) {
  return command.replace(/[\t ]*(?:\r?\n)+$/g, "");
}

function alertRuleTypeChanged() {
  const type = parseInt($("#ruleForm [name=AlertType]").val(), 10) || 0;
  $(".standard-alert-fields").toggle(type === 0);
  $(".expiration-alert-fields").toggle(type === 1);
  $(".subscription-expiration-alert-fields").toggle(type === 2);
}

function alertRuleData() {
  const form = $("#ruleForm");
  const alertType = parseInt(form.find("[name=AlertType]").val(), 10) || 0;
  const data = {
    AlertType: alertType,
    ID: parseInt(form.find("[name=ID]").val(), 10) || 0,
    Name: form.find("[name=Name]").val().trim(),
    NotificationTag: form.find("[name=NotificationTag]").val().trim(),
  };
  if (alertType === 1) {
    const value = form.find("[name=SkipServersRaw]").val() || "";
    data.AdvanceDays = parseInt(form.find("[name=AdvanceDays]").val(), 10) || 0;
    data.DailyReminder = form.find("[name=DailyReminder]").is(":checked") ? "on" : "";
    data.Enable = form.find("[name=ExpirationEnable]").is(":checked") ? "on" : "";
    data.Cover = parseInt(form.find("[name=Cover]").val(), 10) || 0;
    data.SkipServersRaw = JSON.stringify(
      [...String(value).matchAll(/\d+/g)].map(function (match) {
        return parseInt(match[0], 10);
      })
    );
    return data;
  }
  if (alertType === 2) {
    const value = form.find("[name=SkipSubscriptionsRaw]").val() || "";
    data.AdvanceDays = parseInt(form.find("[name=SubscriptionAdvanceDays]").val(), 10) || 0;
    data.DailyReminder = form.find("[name=SubscriptionDailyReminder]").is(":checked") ? "on" : "";
    data.Enable = form.find("[name=SubscriptionExpirationEnable]").is(":checked") ? "on" : "";
    data.Cover = parseInt(form.find("[name=SubscriptionCover]").val(), 10) || 0;
    data.SkipSubscriptionsRaw = JSON.stringify(
      [...String(value).matchAll(/\d+/g)].map(function (match) {
        return parseInt(match[0], 10);
      })
    );
    return data;
  }
  data.RulesRaw = form.find("[name=RulesRaw]").val();
  data.TriggerMode = parseInt(form.find("[name=TriggerMode]").val(), 10) || 0;
  data.FailTriggerTasksRaw = JSON.stringify(
    [...String(form.find("[name=FailTriggerTasksRaw]").val() || "").matchAll(/\d+/g)]
      .map(function (match) { return parseInt(match[0], 10); })
  );
  data.RecoverTriggerTasksRaw = JSON.stringify(
    [...String(form.find("[name=RecoverTriggerTasksRaw]").val() || "").matchAll(/\d+/g)]
      .map(function (match) { return parseInt(match[0], 10); })
  );
  data.Enable = form.find("[name=Enable]").is(":checked") ? "on" : "";
  return data;
}

function addOrEditAlertRule(rule, alertType) {
  const modal = $(".rule.modal");
  const form = $("#ruleForm");
  const type = alertType || 0;
  modal.children(".header").text((rule ? LANG.Edit : LANG.Add) + ' ' + LANG.AlarmRule);
  modal.find(".nezha-primary-btn.button").html(
    rule ? LANG.Edit + '<i class="edit icon"></i>' : LANG.Add + '<i class="add icon"></i>'
  );
  const alertTypeSelect = form.find("select[name=AlertType]");
  setFormSelectValue(alertTypeSelect, String(type));
  const alertTypeDropdown = alertTypeSelect.closest(".ui.dropdown");
  alertTypeSelect.prop("disabled", false);
  alertTypeDropdown.removeClass("disabled");
  alertTypeDropdown.attr("aria-disabled", "false");
  form.find("[name=ID]").val(rule ? rule.ID : 0);
  form.find("[name=Name]").val(rule ? rule.Name : "");
  form.find("[name=NotificationTag]").val(rule ? rule.NotificationTag : "default");
  modal.find("a.ui.label.visible").remove();

  if (type === 0) {
    form.find("[name=RulesRaw]").val(rule ? rule.RulesRaw : "");
    setFormSelectValue(form.find("[name=TriggerMode]"), String(rule ? rule.TriggerMode : 0));
    modal.find(".ui.rule-enable.checkbox").checkbox(
      rule && rule.Enable ? "set checked" : "set unchecked"
    );
    form.find("[name=AdvanceDays]").val(1);
    setFormSelectValue(form.find("[name=Cover]"), "0");
    form.find("[name=SkipServersRaw]").val("");
    form.find("[name=SkipSubscriptionsRaw]").val("");
    modal.find(".expiration-daily-reminder").checkbox("set unchecked");
    modal.find(".expiration-rule-enable").checkbox(
      !rule || rule.Enable ? "set checked" : "set unchecked"
    );
    form.find("[name=SubscriptionAdvanceDays]").val(1);
    setFormSelectValue(form.find("[name=SubscriptionCover]"), "0");
    modal.find(".subscription-expiration-daily-reminder").checkbox("set unchecked");
    modal.find(".subscription-expiration-rule-enable").checkbox("set checked");
    const failRaw = rule ? rule.FailTriggerTasksRaw || "[]" : "[]";
    const recoverRaw = rule ? rule.RecoverTriggerTasksRaw || "[]" : "[]";
    form.find("[name=FailTriggerTasksRaw]").val(failRaw);
    form.find("[name=RecoverTriggerTasksRaw]").val(recoverRaw);
    const node1 = modal.find("i.dropdown.icon.1");
    const node2 = modal.find("i.dropdown.icon.2");
    JSON.parse(failRaw).forEach(function (id) {
      node1.after('<a class="ui label transition visible" data-value="' + id +
        '" style="display: inline-block !important;">ID:' + id + '<i class="delete icon"></i></a>');
    });
    JSON.parse(recoverRaw).forEach(function (id) {
      node2.after('<a class="ui label transition visible" data-value="' + id +
        '" style="display: inline-block !important;">ID:' + id + '<i class="delete icon"></i></a>');
    });
  } else if (type === 1) {
    form.find("[name=RulesRaw]").val("");
    setFormSelectValue(form.find("[name=TriggerMode]"), "0");
    form.find("[name=FailTriggerTasksRaw]").val("[]");
    form.find("[name=RecoverTriggerTasksRaw]").val("[]");
    form.find("[name=SkipSubscriptionsRaw]").val("");
    modal.find(".ui.rule-enable.checkbox").checkbox(
      rule && rule.Enable ? "set checked" : "set unchecked"
    );
    const expirationRule = rule ? JSON.parse(rule.RulesRaw || "[]")[0] || {} : {};
    form.find("[name=AdvanceDays]").val(rule ? expirationRule.advance_days : 1);
    setFormSelectValue(form.find("[name=Cover]"),
      String(rule ? expirationRule.cover || 0 : 0)
    );
    modal.find(".expiration-daily-reminder").checkbox(
      rule && expirationRule.daily_reminder ? "set checked" : "set unchecked"
    );
    modal.find(".expiration-rule-enable").checkbox(
      !rule || rule.Enable ? "set checked" : "set unchecked"
    );
    const serverIDs = Object.keys(expirationRule.ignore || {}).map(function (id) {
      return parseInt(id, 10);
    });
    form.find("[name=SkipServersRaw]").val(serverIDs.join(","));
    const serverIcon = form.find("i.dropdown.icon.expirationSpecificServer");
    serverIDs.forEach(function (id) {
      serverIcon.after('<a class="ui label transition visible" data-value="' + id +
        '" style="display: inline-block !important;">ID:' + id + '<i class="delete icon"></i></a>');
    });
  } else {
    form.find("[name=RulesRaw]").val("");
    setFormSelectValue(form.find("[name=TriggerMode]"), "0");
    form.find("[name=FailTriggerTasksRaw]").val("[]");
    form.find("[name=RecoverTriggerTasksRaw]").val("[]");
    form.find("[name=SkipServersRaw]").val("");
    const expirationRule = rule ? JSON.parse(rule.RulesRaw || "[]")[0] || {} : {};
    form.find("[name=SubscriptionAdvanceDays]").val(rule ? expirationRule.advance_days : 1);
    setFormSelectValue(form.find("[name=SubscriptionCover]"),
      String(rule ? expirationRule.cover || 0 : 0)
    );
    modal.find(".subscription-expiration-daily-reminder").checkbox(
      rule && expirationRule.daily_reminder ? "set checked" : "set unchecked"
    );
    modal.find(".subscription-expiration-rule-enable").checkbox(
      !rule || rule.Enable ? "set checked" : "set unchecked"
    );
    const subscriptionIDs = Object.keys(expirationRule.ignore || {}).map(function (id) {
      return parseInt(id, 10);
    });
    form.find("[name=SkipSubscriptionsRaw]").val(subscriptionIDs.join(","));
    const subscriptionIcon = form.find("i.dropdown.icon.subscriptionExpirationSpecific");
    subscriptionIDs.forEach(function (id) {
      subscriptionIcon.after('<a class="ui label transition visible" data-value="' + id +
        '" style="display: inline-block !important;">ID:' + id + '<i class="delete icon"></i></a>');
    });
  }
  alertRuleTypeChanged();
  showFormModal(".rule.modal", "#ruleForm", "/api/alert-rule", alertRuleData);
}

function addOrEditNotification(notification) {
  const modal = $(".notification.modal");
  const current = notification || {};
  modal.children(".header").text((notification ? LANG.Edit : LANG.Add) + ' ' + LANG.Notification);
  modal
    .find(".nezha-primary-btn.button")
    .html(
      notification
        ? LANG.Edit + '<i class="edit icon"></i>'
        : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=ID]").val(notification ? notification.ID : null);
  modal.find("input[name=Name]").val(notification ? notification.Name : null);
  modal.find("input[name=Tag]").val(notification ? notification.Tag : null);
  setFormSelectValue(modal.find("select[name=Type]"), String(current.Type || 0));
  modal.find("input[name=URL]").val(notification ? notification.URL : null);
  modal
    .find("textarea[name=RequestHeader]")
    .val(notification ? notification.RequestHeader : null);
  modal
    .find("textarea[name=RequestBody]")
    .val(notification ? notification.RequestBody : null);
  modal
    .find("select[name=RequestMethod]")
    .val(notification ? notification.RequestMethod : 1);
  modal
    .find("select[name=RequestType]")
    .val(notification ? notification.RequestType : 1);
  modal.find("input[name=TelegramToken]").val(current.TelegramToken || "");
  modal.find("input[name=TelegramChatID]").val(current.TelegramChatID || "");
  modal.find("input[name=SMTPHost]").val(current.SMTPHost || "");
  modal.find("input[name=SMTPPort]").val(current.SMTPPort || 465);
  modal.find("input[name=SMTPUsername]").val(current.SMTPUsername || "");
  modal.find("input[name=SMTPPassword]").val(current.SMTPPassword || "");
  modal.find("input[name=EmailTo]").val(current.EmailTo || "");
  if (notification && notification.VerifySSL) {
    modal.find(".ui.nf-ssl.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.nf-ssl.checkbox").checkbox("set unchecked");
  }
  modal.find(".ui.nf-smtp-tls.checkbox").checkbox(
    notification && !current.SMTPTLS ? "set unchecked" : "set checked"
  );
  modal.find(".ui.nf-skip-check.checkbox").checkbox("set unchecked");
  notificationTypeChanged();
  showFormModal(
    ".notification.modal",
    "#notificationForm",
    "/api/notification"
  );
}

function notificationTypeChanged() {
  const type = parseInt($("#notificationForm [name=Type]").val(), 10) || 0;
  $(".notification-webhook-fields").toggle(type === 0);
  $(".notification-telegram-fields").toggle(type === 1);
  $(".notification-email-fields").toggle(type === 2);
  $(".notification-test-button").toggle(type !== 0);
}

function notificationFormData() {
  return $("#notificationForm")
    .serializeArray()
    .reduce(function (data, item) {
      data[item.name] = item.name === "ID" || item.name === "Type" ||
        item.name === "RequestMethod" || item.name === "RequestType" ||
        item.name === "SMTPPort"
        ? parseInt(item.value, 10) || 0
        : item.value;
      return data;
    }, {});
}

function testNotificationMethod(button) {
  if (button.hasClass("loading")) {
    return;
  }
  button.addClass("loading");
  const modal = $(".notification.modal");
  $.ajax({
    url: "/api/notification/verify",
    type: "POST",
    contentType: "application/json",
    data: JSON.stringify(notificationFormData()),
  })
    .done(function (resp) {
      alert(
        resp.code === 200
          ? modal.attr("data-test-success")
          : modal.attr("data-test-failed") + ": " + resp.message
      );
    })
    .fail(function (err) {
      alert(err.responseText);
    })
    .always(function () {
      button.removeClass("loading");
    });
}

function addOrEditDDNS(ddns) {
  const modal = $(".ddns.modal");
  modal.children(".header").text((ddns ? LANG.Edit : LANG.Add));
  modal
    .find(".nezha-primary-btn.button")
    .html(
      ddns
        ? LANG.Edit + '<i class="edit icon"></i>'
        : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=ID]").val(ddns ? ddns.ID : null);
  modal.find("input[name=Name]").val(ddns ? ddns.Name : null);
  modal.find("input[name=DomainsRaw]").val(ddns ? ddns.DomainsRaw : null);
  modal.find("input[name=AccessID]").val(ddns ? ddns.AccessID : null);
  modal.find("input[name=AccessSecret]").val(ddns ? ddns.AccessSecret : null);
  modal.find("input[name=MaxRetries]").val(ddns ? ddns.MaxRetries : 3);
  modal.find("input[name=WebhookURL]").val(ddns ? ddns.WebhookURL : null);
  modal
    .find("textarea[name=WebhookHeaders]")
    .val(ddns ? ddns.WebhookHeaders : null);
  modal
    .find("textarea[name=WebhookRequestBody]")
    .val(ddns ? ddns.WebhookRequestBody : null);
  modal
    .find("select[name=Provider]")
    .val(ddns ? ddns.Provider : 0);
  modal
    .find("select[name=WebhookMethod]")
    .val(ddns ? ddns.WebhookMethod : 1);
  modal
    .find("select[name=WebhookRequestType]")
    .val(ddns ? ddns.WebhookRequestType : 1);
  if (ddns && ddns.EnableIPv4) {
    modal.find(".ui.enableipv4.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.enableipv4.checkbox").checkbox("set unchecked");
  }
  if (ddns && ddns.EnableIPv6) {
    modal.find(".ui.enableipv6.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.enableipv6.checkbox").checkbox("set unchecked");
  }
  showFormModal(
    ".ddns.modal",
    "#ddnsForm",
    "/api/ddns"
  );
}

function addOrEditNAT(nat) {
  requireTwoFactorGrant(function () {
    showNATModal(nat);
  });
}

function showNATModal(nat) {
  const modal = $(".nat.modal");
  modal.children(".header").text((nat ? LANG.Edit : LANG.Add));
  modal
    .find(".nezha-primary-btn.button")
    .html(
      nat
        ? LANG.Edit + '<i class="edit icon"></i>'
        : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=ID]").val(nat ? nat.ID : null);
  modal.find("input[name=ServerID]").val(nat ? nat.ServerID : null);
  modal.find("input[name=Name]").val(nat ? nat.Name : null);
  modal.find("input[name=Host]").val(nat ? nat.Host : null);
  modal.find("input[name=Domain]").val(nat ? nat.Domain : null);
  showFormModal(
    ".nat.modal",
    "#natForm",
    "/api/nat"
  );
}

// 最近一次得知的授权到期时间（毫秒），未启用双重验证时为 Infinity，未知时为 null。
// 供必须在用户手势内同步决策的场景（打开终端新窗口）使用，以服务端校验为准
let twoFactorGrantExpiresAt = null;

function updateTwoFactorGrantState(result) {
  if (!result) return;
  twoFactorGrantExpiresAt = result.enabled ? Date.now() + (result.grantRemaining || 0) * 1000 : Infinity;
}

function refreshTwoFactorGrantState() {
  return $.get("/api/totp").done(function (resp) {
    if (resp.code == 200) updateTwoFactorGrantState(resp.result);
  });
}

// 敏感操作二次验证授权：启用双重验证后，终端、文件管理、计划任务、NAT
// 需在验证通过后的 30 分钟内操作；已授权或无需授权时直接执行 callback。
function requireTwoFactorGrant(callback) {
  if ($(".two-factor-grant.modal").length === 0) {
    callback();
    return;
  }
  refreshTwoFactorGrantState()
    .done(function (resp) {
      if (resp.code != 200) {
        alert("操作失败 " + resp.code + "：" + resp.message);
      } else if (!resp.result || !resp.result.enabled || resp.result.grantRemaining > 0) {
        callback();
      } else {
        showTwoFactorGrantModal(callback);
      }
    })
    .fail(function (err) {
      alert("网络错误：" + err.responseText);
    });
}

// 通行密钥（WebAuthn）。服务端返回 base64url 编码的选项，浏览器 API 需要 ArrayBuffer；
// 提交给服务端的结果再编码回 base64url JSON。
const nzPasskey = (function () {
  function toBuffer(value) {
    const base64 = String(value).replace(/-/g, "+").replace(/_/g, "/");
    const binary = atob(base64 + "===".slice((base64.length + 3) % 4));
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes.buffer;
  }

  function toBase64URL(buffer) {
    const bytes = new Uint8Array(buffer);
    let binary = "";
    for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function decodeDescriptors(list) {
    return (list || []).map(function (d) {
      return Object.assign({}, d, { id: toBuffer(d.id) });
    });
  }

  function supported() {
    return !!(window.isSecureContext && window.PublicKeyCredential && navigator.credentials);
  }

  function postJSON(url, data) {
    return Promise.resolve($.ajax({
      url: url,
      type: "POST",
      contentType: "application/json",
      data: JSON.stringify(data || {}),
    }));
  }

  // 取出 {code, message, result: {publicKey}} 响应中的 WebAuthn 选项
  function unwrap(resp) {
    if (!resp || resp.code != 200 || !resp.result || !resp.result.publicKey) {
      throw new Error((resp && resp.message) || "通行密钥请求失败");
    }
    return resp.result.publicKey;
  }

  function ensureSupported() {
    if (!supported()) throw new Error("当前浏览器或访问地址不支持通行密钥，请使用 HTTPS 域名访问");
  }

  // 调用 navigator.credentials.get，返回可提交给服务端的断言 JSON 字符串
  async function get(options) {
    ensureSupported();
    const publicKey = Object.assign({}, options, { challenge: toBuffer(options.challenge) });
    if (options.allowCredentials) publicKey.allowCredentials = decodeDescriptors(options.allowCredentials);
    const cred = await navigator.credentials.get({ publicKey: publicKey });
    if (!cred) throw new Error("通行密钥操作已取消");
    const r = cred.response;
    return JSON.stringify({
      id: cred.id,
      rawId: toBase64URL(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
      response: {
        authenticatorData: toBase64URL(r.authenticatorData),
        clientDataJSON: toBase64URL(r.clientDataJSON),
        signature: toBase64URL(r.signature),
        userHandle: r.userHandle ? toBase64URL(r.userHandle) : undefined,
      },
    });
  }

  // 调用 navigator.credentials.create，返回可提交给服务端的注册结果对象
  async function create(options) {
    ensureSupported();
    const publicKey = Object.assign({}, options, {
      challenge: toBuffer(options.challenge),
      user: Object.assign({}, options.user, { id: toBuffer(options.user.id) }),
      excludeCredentials: decodeDescriptors(options.excludeCredentials),
    });
    const cred = await navigator.credentials.create({ publicKey: publicKey });
    if (!cred) throw new Error("通行密钥操作已取消");
    const r = cred.response;
    return {
      id: cred.id,
      rawId: toBase64URL(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
      response: {
        attestationObject: toBase64URL(r.attestationObject),
        clientDataJSON: toBase64URL(r.clientDataJSON),
        transports: r.getTransports ? r.getTransports() : [],
      },
    };
  }

  // 已登录会话的二次验证：返回断言 JSON 字符串，随敏感操作请求一并提交
  function verify() {
    try {
      ensureSupported();
    } catch (err) {
      return Promise.reject(err);
    }
    return postJSON("/api/passkey", { operation: "verify-options" }).then(unwrap).then(get);
  }

  function errorMessage(err) {
    if (err && err.name === "NotAllowedError") return "通行密钥操作已取消或超时";
    if (err && err.name === "InvalidStateError") return "该认证器已添加过通行密钥";
    if (err && err.name === "SecurityError") return "当前访问地址不支持通行密钥，请使用 HTTPS 域名访问";
    if (err && err.responseText !== undefined) return "网络错误：" + (err.responseText || err.status);
    return (err && err.message) || String(err);
  }

  // Signal API：通知浏览器（及背后的密码管理器）某个通行密钥已在服务端删除，由其移除或隐藏。
  // 仅在设置页手动删除成功后调用；浏览器不支持、或凭据不属于当前域名（浏览器不允许跨域通知）时静默跳过。
  // 始终返回成功的 Promise，结果不影响后续流程。
  function signalUnknown(rpId, credentialId) {
    try {
      if (!rpId || !credentialId || rpId !== window.location.hostname ||
        !window.PublicKeyCredential || typeof PublicKeyCredential.signalUnknownCredential !== "function") {
        return Promise.resolve();
      }
      return PublicKeyCredential.signalUnknownCredential({ rpId: rpId, credentialId: credentialId }).catch(function () {});
    } catch (err) {
      return Promise.resolve();
    }
  }

  return { supported: supported, postJSON: postJSON, unwrap: unwrap, get: get, create: create, verify: verify, signalUnknown: signalUnknown, errorMessage: errorMessage };
})();

function verifyTwoFactorGrant(code) {
  return $.ajax({
    url: "/api/totp",
    type: "POST",
    contentType: "application/json",
    data: JSON.stringify({ operation: "grant", code: code }),
  }).done(function (resp) {
    if (resp.code == 200) updateTwoFactorGrantState(resp.result);
  });
}

// 用通行密钥代替动态验证码完成敏感操作授权，返回与 verifyTwoFactorGrant 相同结构的响应
function verifyTwoFactorGrantWithPasskey() {
  return nzPasskey
    .verify()
    .then(function (assertion) {
      return nzPasskey.postJSON("/api/passkey", { operation: "grant", passkey: assertion });
    })
    .then(function (resp) {
      if (resp.code == 200) updateTwoFactorGrantState(resp.result);
      return resp;
    });
}

// openWindow 为 true 时，callback 以窗口名为参数，向该窗口提交以打开新页面
function showTwoFactorGrantModal(callback, openWindow) {
  const modal = $(".two-factor-grant.modal");
  const form = modal.find("form");
  const btn = modal.find(".nezha-primary-btn.button");
  const input = form.find("input[name=TwoFactorCode]");
  let verified = false;
  let pendingOpen = false;
  form.children(".message").remove();
  input.val("");
  btn.removeClass("loading");
  input.off("keydown.twoFactorGrant").on("keydown.twoFactorGrant", function (e) {
    if (e.key === "Enter") {
      e.preventDefault();
      btn.trigger("click");
    }
  });
  // 通行密钥代替动态验证码完成授权。通行密钥验证需要当前页面保持焦点，不预开窗口
  const passkeyBtn = modal.find(".passkey-verify.button");
  passkeyBtn.removeClass("loading");
  passkeyBtn.off("click.twoFactorGrant").on("click.twoFactorGrant", function (e) {
    e.preventDefault();
    if (btn.hasClass("loading") || passkeyBtn.hasClass("loading")) {
      return;
    }
    // 已验证但新窗口被拦截：本次点击是新的用户手势，直接打开
    if (pendingOpen) {
      callback("_blank");
      modal.modal("hide");
      return;
    }
    form.children(".message").remove();
    passkeyBtn.addClass("loading");
    verifyTwoFactorGrantWithPasskey()
      .then(function (resp) {
        if (resp.code != 200) {
          form.append($('<div class="ui negative message"></div>').text(resp.message));
          return;
        }
        if (!openWindow) {
          verified = true;
          modal.modal("hide");
          return;
        }
        if (!navigator.userActivation || navigator.userActivation.isActive) {
          callback("_blank");
          modal.modal("hide");
          return;
        }
        pendingOpen = true;
        form.append($('<div class="ui positive message"></div>').text("验证已通过，请再次点击确认打开"));
      })
      .catch(function (err) {
        form.append($('<div class="ui negative message"></div>').text(nzPasskey.errorMessage(err)));
      })
      .finally(function () {
        passkeyBtn.removeClass("loading");
      });
  });
  modal
    .modal({
      closable: true,
      autofocus: false,
      onVisible: function () {
        input.trigger("focus");
      },
      onApprove: function () {
        if (btn.hasClass("loading")) {
          return false;
        }
        // 已验证但新窗口被拦截：本次点击是新的用户手势，直接打开
        if (pendingOpen) {
          callback("_blank");
          modal.modal("hide");
          return false;
        }
        form.children(".message").remove();
        btn.addClass("loading");
        const targetName = "nezha-grant-" + Date.now();
        // 触屏浏览器在异步回调里打开新窗口会被拦截，点击确认时先同步打开空白窗口；
        // 桌面浏览器点击后数秒内仍允许打开，验证通过后再开，避免先跳到空白页
        let target = openWindow && window.matchMedia("(pointer: coarse)").matches ? window.open("", targetName) : null;
        verifyTwoFactorGrant(input.val())
          .done(function (resp) {
            if (resp.code != 200) {
              form.append($('<div class="ui negative message"></div>').text(resp.message));
              return;
            }
            if (!openWindow) {
              verified = true;
              modal.modal("hide");
              return;
            }
            if (target) {
              callback(targetName);
              target = null;
              modal.modal("hide");
              return;
            }
            // 桌面端直接以 _blank 提交，不经 window.open 预开 about:blank，避免先出现空白标签页；
            // 点击确认的瞬时激活已失效时提交会被静默拦截，改为提示再点一次
            if (!navigator.userActivation || navigator.userActivation.isActive) {
              callback("_blank");
              modal.modal("hide");
              return;
            }
            pendingOpen = true;
            form.append($('<div class="ui positive message"></div>').text("验证已通过，请再次点击确认打开"));
          })
          .fail(function (err) {
            form.append($('<div class="ui negative message"></div>').text("网络错误：" + err.responseText));
          })
          .always(function () {
            if (target) target.close();
            btn.removeClass("loading");
          });
        return false;
      },
      // 等验证弹窗完全关闭后再继续，避免与随后打开的表单弹窗冲突
      onHidden: function () {
        if (verified) {
          callback();
        }
      },
    })
    .modal("show");
}

// 终端在新窗口打开，必须在点击事件内同步决策：经 requireTwoFactorGrant 异步回调后再打开会失去用户手势，
// 移动端浏览器会按弹窗拦截。授权状态未知或缓存与服务端不一致时，由 /terminal 在新标签页内显示验证页兜底
function connectToServer(id) {
  const params = { Host: window.location.host, Protocol: window.location.protocol, ID: id };
  if ($(".two-factor-grant.modal").length === 0 || twoFactorGrantExpiresAt === null || twoFactorGrantExpiresAt > Date.now()) {
    post('/terminal', params);
    return;
  }
  showTwoFactorGrantModal(function (target) {
    post('/terminal', params, 'post', target);
  }, true);
}

$(function () {
  if ($(".two-factor-grant.modal").length > 0 && $("[onclick^='connectToServer']").length > 0) {
    refreshTwoFactorGrantState();
  }
});

function post(path, params, method = 'post', target = '_blank') {
  const form = document.createElement('form');
  form.method = method;
  form.action = path;
  form.target = target;

  for (const key in params) {
    if (params.hasOwnProperty(key)) {
      const hiddenField = document.createElement('input');
      hiddenField.type = 'hidden';
      hiddenField.name = key;
      hiddenField.value = params[key];
      form.appendChild(hiddenField);
    }
  }
  const csrfToken = getCSRFToken();
  if (csrfToken) {
    const csrfField = document.createElement('input');
    csrfField.type = 'hidden';
    csrfField.name = '_csrf';
    csrfField.value = csrfToken;
    form.appendChild(csrfField);
  }
  document.body.appendChild(form);
  form.submit();
  document.body.removeChild(form);
}

function issueNewApiToken(apiToken) {
  const modal = $(".api.modal");
  modal.children(".header").text((apiToken ? LANG.Edit : LANG.Add) + ' ' + "API Token");
  modal
    .find(".nezha-primary-btn.button")
    .html(
      apiToken ? LANG.Edit + '<i class="edit icon"></i>' : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=Note]").val(apiToken ? apiToken.Note : null);
  modal.find("input[name=TwoFactorCode]").val("");
  showFormModal(".api.modal", "#apiForm", "/api/token", null, showIssuedApiToken);
}

// 用通行密钥代替动态验证码签发 API Token：断言写入隐藏字段后触发确认，
// 表单在点击确认时同步读取，随后立即清空（断言只能使用一次）
function issueNewApiTokenWithPasskey(button) {
  const $btn = $(button);
  const modal = $(".api.modal");
  const field = modal.find("input[name=PasskeyAssertion]");
  if ($btn.hasClass("loading")) return;
  modal.find("form").children(".message").remove();
  $btn.addClass("loading");
  nzPasskey.verify()
    .then(function (assertion) {
      field.val(assertion);
      modal.find(".nezha-primary-btn.button").trigger("click");
    })
    .catch(function (err) {
      modal.find("form").append(
        $('<div class="ui negative message"></div>').text(nzPasskey.errorMessage(err))
      );
    })
    .finally(function () {
      field.val("");
      $btn.removeClass("loading");
    });
}

function showIssuedApiToken(resp) {
  const token = resp && resp.result ? resp.result.token : "";
  if (!token) {
    window.location.reload();
    return;
  }
  $(".api.modal").modal("hide");
  const modal = $(".issued-api-token.modal");
  modal.find("input[name=IssuedToken]").val(token);
  setTimeout(function () {
    modal.modal({
      closable: false,
      onHidden: function () {
        modal.find("input[name=IssuedToken]").val("");
        window.location.reload();
      },
    }).modal("show");
  }, 350);
}

function copyIssuedApiToken(button) {
  const input = $(button).siblings("input[name=IssuedToken]")[0];
  if (!input) return;
  input.select();
  input.setSelectionRange(0, input.value.length);
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(input.value);
  } else {
    document.execCommand("copy");
  }
}

function addOrEditServer(server, conf) {
  const modal = $(".server.modal");
  modal.children(".header").text((server ? LANG.Edit : LANG.Add) + ' ' + LANG.Server);
  modal
    .find(".nezha-primary-btn.button")
    .html(
      server ? LANG.Edit + '<i class="edit icon"></i>' : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=id]").val(server ? server.ID : null);
  modal.find("input[name=name]").val(server ? server.Name : null);
  modal.find("input[name=Tag]").val(server ? server.Tag : null);
  modal.find("a.ui.label.visible").each((i, el) => {
    el.remove();
  });
  var ddns;
  if (server) {
    ddns = server.DDNSProfilesRaw;
    let serverList;
    try {
      serverList = JSON.parse(ddns);
    } catch (error) {
      serverList = "[]";
    }
    const node = modal.find("i.dropdown.icon.ddnsProfiles");
    for (let i = 0; i < serverList.length; i++) {
      node.after(
        '<a class="ui label transition visible" data-value="' +
        serverList[i] +
        '" style="display: inline-block !important;">ID:' +
        serverList[i] +
        '<i class="delete icon"></i></a>'
      );
    }
  }
  // 需要在 showFormModal 进一步拼接数组
  modal
    .find("input[name=DDNSProfilesRaw]")
    .val(server ? "[]," + ddns.substr(1, ddns.length - 2) : "[]");
  modal
    .find("input[name=DisplayIndex]")
    .val(server ? server.DisplayIndex : null);
  modal.find("textarea[name=Note]").val(server ? server.Note : null);
  modal.find("textarea[name=PublicNote]").val(server ? server.PublicNote : null);
  modal.find("input[name=Link]").val(server ? server.Link || "" : "");
  if (server) {
    modal.find(".secret.field").attr("style", "");
    modal.find(".command.field").attr("style", "");
    modal.find(".command.hostSecret").text(server.Secret);
    modal.find("input[name=secret]").val(server.Secret);
  } else {
    modal.find(".secret.field").attr("style", "display:none");
    modal.find(".command.field").attr("style", "display:none");
    modal.find("input[name=secret]").val("");
  }
  if (server && server.EnableDDNS) {
    modal.find(".ui.enableddns.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.enableddns.checkbox").checkbox("set unchecked");
  }
  if (server && server.HideForGuest) {
    modal.find(".ui.hideforguest.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.hideforguest.checkbox").checkbox("set unchecked");
  }

  showFormModal(".server.modal", "#serverForm", "/api/server");
}

function addOrEditMonitor(monitor) {
  const modal = $(".monitor.modal");
  modal.children(".header").text((monitor ? LANG.Edit : LANG.Add) + ' ' + LANG.Monitor);
  modal
    .find(".nezha-primary-btn.button")
    .html(
      monitor ? LANG.Edit + '<i class="edit icon"></i>' : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=ID]").val(monitor ? monitor.ID : null);
  modal.find("input[name=Name]").val(monitor ? monitor.Name : null);
  modal.find("input[name=Target]").val(monitor ? monitor.Target : null);
  modal.find("input[name=Duration]").val(monitor && monitor.Duration ? monitor.Duration : 30);
  modal.find("select[name=Type]").val(monitor ? monitor.Type : 1);
  modal.find("select[name=Cover]").val(monitor ? monitor.Cover : 0);
  modal.find("input[name=NotificationTag]").val(monitor ? monitor.NotificationTag : null);
  if (monitor && monitor.EnableShowInService) {
    modal.find(".ui.nb-show-in-service.checkbox").checkbox("set checked")
  } else {
    modal.find(".ui.nb-show-in-service.checkbox").checkbox("set unchecked")
  }
  if (monitor && monitor.Notify) {
    modal.find(".ui.nb-notify.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.nb-notify.checkbox").checkbox("set unchecked");
  }
  modal.find("input[name=MaxLatency]").val(monitor ? monitor.MaxLatency : null);
  modal.find("input[name=MinLatency]").val(monitor ? monitor.MinLatency : null);
  if (monitor && monitor.LatencyNotify) {
    modal.find(".ui.nb-lt-notify.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.nb-lt-notify.checkbox").checkbox("set unchecked");
  }
  modal.find("a.ui.label.visible").each((i, el) => {
    el.remove();
  });
  if (monitor && monitor.EnableTriggerTask) {
    modal.find(".ui.nb-EnableTriggerTask.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.nb-EnableTriggerTask.checkbox").checkbox("set unchecked");
  }
  var servers;
  var failTriggerTasks;
  var recoverTriggerTasks;
  if (monitor) {
    servers = monitor.SkipServersRaw;
    const serverList = JSON.parse(servers || "[]");
    const node = modal.find("i.dropdown.icon.specificServer");
    for (let i = 0; i < serverList.length; i++) {
      node.after(
        '<a class="ui label transition visible" data-value="' +
        serverList[i] +
        '" style="display: inline-block !important;">ID:' +
        serverList[i] +
        '<i class="delete icon"></i></a>'
      );
    }

    failTriggerTasks = monitor.FailTriggerTasksRaw;
    recoverTriggerTasks = monitor.RecoverTriggerTasksRaw;
    const failTriggerTasksList = JSON.parse(failTriggerTasks || "[]");
    const recoverTriggerTasksList = JSON.parse(recoverTriggerTasks || "[]");
    const node1 = modal.find("i.dropdown.icon.failTask");
    const node2 = modal.find("i.dropdown.icon.recoverTask");
    for (let i = 0; i < failTriggerTasksList.length; i++) {
      node1.after(
        '<a class="ui label transition visible" data-value="' +
        failTriggerTasksList[i] +
        '" style="display: inline-block !important;">ID:' +
        failTriggerTasksList[i] +
        '<i class="delete icon"></i></a>'
      );
    }
    for (let i = 0; i < recoverTriggerTasksList.length; i++) {
      node2.after(
        '<a class="ui label transition visible" data-value="' +
        recoverTriggerTasksList[i] +
        '" style="display: inline-block !important;">ID:' +
        recoverTriggerTasksList[i] +
        '<i class="delete icon"></i></a>'
      );
    }
  }
  // 需要在 showFormModal 进一步拼接数组
  modal
    .find("input[name=FailTriggerTasksRaw]")
    .val(monitor ? "[]," + failTriggerTasks.substr(1, failTriggerTasks.length - 2) : "[]");
  modal
    .find("input[name=RecoverTriggerTasksRaw]")
    .val(monitor ? "[]," + recoverTriggerTasks.substr(1, recoverTriggerTasks.length - 2) : "[]");

  modal
    .find("input[name=SkipServersRaw]")
    .val(monitor ? "[]," + servers.substr(1, servers.length - 2) : "[]");
  showFormModal(".monitor.modal", "#monitorForm", "/api/monitor");
}

function addOrEditCron(cron) {
  requireTwoFactorGrant(function () {
    showCronModal(cron);
  });
}

function showCronModal(cron) {
  const modal = $(".cron.modal");
  modal.children(".header").text((cron ? LANG.Edit : LANG.Add) + ' ' + LANG.Cron);
  modal
    .find(".nezha-primary-btn.button")
    .html(
      cron ? LANG.Edit + '<i class="edit icon"></i>' : LANG.Add + '<i class="add icon"></i>'
    );
  modal.find("input[name=ID]").val(cron ? cron.ID : null);
  modal.find("input[name=Name]").val(cron ? cron.Name : null);
  modal.find("select[name=TaskType]").val(cron ? cron.TaskType : 0);
  modal.find("select[name=Cover]").val(cron ? cron.Cover : 0);
  modal.find("input[name=NotificationTag]").val(cron ? cron.NotificationTag : null);
  modal.find("input[name=Scheduler]").val(cron ? cron.Scheduler : null);
  modal.find("a.ui.label.visible").each((i, el) => {
    el.remove();
  });
  var servers;
  if (cron) {
    servers = cron.ServersRaw;
    const serverList = JSON.parse(servers || "[]");
    const node = modal.find("i.dropdown.icon");
    for (let i = 0; i < serverList.length; i++) {
      node.after(
        '<a class="ui label transition visible" data-value="' +
        serverList[i] +
        '" style="display: inline-block !important;">ID:' +
        serverList[i] +
        '<i class="delete icon"></i></a>'
      );
    }
  }
  // 需要在 showFormModal 进一步拼接数组
  modal
    .find("input[name=ServersRaw]")
    .val(cron ? "[]," + servers.substr(1, servers.length - 2) : "[]");
  modal.find("textarea[name=Command]").val(cron ? cron.Command : null);
  if (cron && cron.PushSuccessful) {
    modal.find(".ui.push-successful.checkbox").checkbox("set checked");
  } else {
    modal.find(".ui.push-successful.checkbox").checkbox("set unchecked");
  }
  showFormModal(".cron.modal", "#cronForm", "/api/cron");
}

function deleteRequest(api) {
  $.ajax({
    url: api,
    type: "DELETE",
  })
    .done((resp) => {
      if (resp.code == 200) {
        if (resp.message) {
          alert(resp.message);
        } else {
          alert("删除成功");
        }
        window.location.reload();
      } else {
        alert("删除失败 " + resp.code + "：" + resp.message);
        confirmBtn.toggleClass("loading");
      }
    })
    .fail((err) => {
      alert("网络错误：" + err.responseText);
    });
}

function manualTrigger(btn, cronId) {
  requireTwoFactorGrant(function () {
    triggerCron(btn, cronId);
  });
}

function triggerCron(btn, cronId) {
  $(btn).toggleClass("loading");
  $.ajax({
    url: "/api/cron/" + cronId + "/manual",
    type: "POST",
  })
    .done((resp) => {
      $(btn).toggleClass("loading");
      if (resp.code == 200) {
        $.suiAlert({
          title: "触发成功，等待执行结果",
          type: "success",
          description: "Success",
          time: "0.5",
          position: "top-center",
        });
      } else {
        $.suiAlert({
          title: "触发失败 ",
          type: "error",
          description: resp.code + "：" + resp.message,
          time: "3",
          position: "top-center",
        });
      }
    })
    .fail((err) => {
      $(btn).toggleClass("loading");
      $.suiAlert({
        title: "触发失败 ",
        type: "error",
        description: "网络错误：" + err.responseText,
        time: "3",
        position: "top-center",
      });
    });
}

function logout(id) {
  $.post("/api/logout", JSON.stringify({ id: id }))
    .done(function (resp) {
      if (resp.code == 200) {
        $.suiAlert({
          title: "注销成功",
          type: "success",
          description: "如需继续访问请使用 GitHub 再次登录",
          time: "3",
          position: "top-center",
        });
        window.location.reload();
      } else {
        $.suiAlert({
          title: "注销失败",
          description: resp.code + "：" + resp.message,
          type: "error",
          time: "3",
          position: "top-center",
        });
      }
    })
    .fail(function (err) {
      $.suiAlert({
        title: "网络错误",
        description: err.responseText,
        type: "error",
        time: "3",
        position: "top-center",
      });
    });
}

$(document).ready(() => {
  try {
    // Semantic UI 的远程下拉菜单默认用 innerHTML 渲染结果；服务器名称
    // 可由 API 创建，菜单和标签均须按纯文本渲染（包括缓存回显）。
    function escapeServerSearchHtml(value) {
      return String(value)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
    }

    $(".ui.servers.search.dropdown").dropdown({
      clearable: true,
      preserveHTML: false,
      templates: {
        menu: function (data, fields) {
          return (data[fields.values] || []).map(function (item) {
            return '<div class="' + (item[fields.disabled] ? 'disabled ' : '') +
              'item" data-value="' + escapeServerSearchHtml(item[fields.value]) +
              '" data-text="' + escapeServerSearchHtml(item[fields.text] || item[fields.name]) +
              '">' + escapeServerSearchHtml(item[fields.name]) + '</div>';
          }).join('');
        },
        label: function (value, text) {
          return escapeServerSearchHtml(text) + '<i class="delete icon"></i>';
        },
      },
      apiSettings: {
        url: "/api/search-server?word={query}",
        cache: false,
      },
    });
  } catch (error) { }
});

$(document).ready(() => {
  try {
    $(".ui.subscriptions.search.dropdown").dropdown({
      clearable: true,
      apiSettings: {
        url: "/api/search-subscription?word={query}",
        cache: false,
      },
    });
  } catch (error) { }
});

$(document).ready(() => {
  try {
    $(".ui.tasks.search.dropdown").dropdown({
      clearable: true,
      apiSettings: {
        url: "/api/search-tasks?word={query}",
        cache: false,
      },
    });
  } catch (error) { }
});

$(document).ready(() => {
  try {
    $(".ui.ddns.search.dropdown").dropdown({
      clearable: true,
      apiSettings: {
        url: "/api/search-ddns?word={query}",
        cache: false,
      },
    });
  } catch (error) { }
});
