(() => {
  function readCookie(name) {
    const match = document.cookie.match(new RegExp("(?:^|;)\\s*" + name + "=([^;]+)"));
    return match ? decodeURIComponent(match[1]) : "";
  }

  function fillCSRF() {
    const token = readCookie("csrf_token");
    if (!token) return;
    document.querySelectorAll(".js-csrf").forEach((el) => {
      el.value = token;
    });
  }

  function initLogsPage() {
    const root = document.querySelector("[data-logs-page]");
    if (!root || root.dataset.logsInitialized === "true") return;

    const container = root.querySelector('[data-logs-role="container"]');
    const statusEl = root.querySelector('[data-logs-role="status"]');
    const btnPause = root.querySelector('[data-logs-role="pause"]');
    const btnDownload = root.querySelector('[data-logs-role="download"]');
    const componentSelect = root.querySelector('[data-logs-role="component"]');
    const levelSelect = root.querySelector('[data-logs-role="level"]');
    const levelButtons = Array.from(root.querySelectorAll("[data-level]"));
    const searchInput = root.querySelector('[data-logs-role="search"]');
    const autoScrollButton = root.querySelector('[data-logs-role="autoscroll"]');
    const bufferedCount = root.querySelector('[data-logs-role="count-buffered"]');
    const infoCount = root.querySelector('[data-logs-role="count-info"]');
    const warnCount = root.querySelector('[data-logs-role="count-warn"]');
    const errorCount = root.querySelector('[data-logs-role="count-error"]');
    const footerSummary = root.querySelector('[data-logs-role="summary"]');
    const footerChip = root.querySelector('[data-logs-role="buffer-chip"]');
    const basePath = root.dataset.panelPath;

    if (
      !container ||
      !statusEl ||
      !btnPause ||
      !btnDownload ||
      !componentSelect ||
      !levelSelect ||
      !levelButtons.length ||
      !searchInput ||
      !autoScrollButton ||
      !bufferedCount ||
      !infoCount ||
      !warnCount ||
      !errorCount ||
      !footerSummary ||
      !footerChip ||
      !basePath
    ) {
      return;
    }

    root.dataset.logsInitialized = "true";

    let paused = false;
    let autoScroll = true;
    let searchTimer = 0;
    let reconnectTimer = 0;
    let ws = null;
    let counts = { buffered: 0, info: 0, warn: 0, error: 0 };

    function renderLevelButtons() {
      levelButtons.forEach((button) => {
        const active = button.dataset.level === levelSelect.value;
        button.classList.toggle("active", active);
      });
    }

    function renderAutoScrollButton() {
      autoScrollButton.classList.toggle("active", autoScroll);
      autoScrollButton.setAttribute("aria-pressed", autoScroll ? "true" : "false");
    }

    function renderCounts() {
      bufferedCount.textContent = String(counts.buffered);
      infoCount.textContent = String(counts.info);
      warnCount.textContent = String(counts.warn);
      errorCount.textContent = String(counts.error);
      footerChip.textContent = `${counts.buffered} buffered`;
      footerSummary.textContent =
        `${componentSelect.value} · ${levelSelect.value} · ${counts.buffered} visible lines`;
    }

    function resetBuffer() {
      counts = { buffered: 0, info: 0, warn: 0, error: 0 };
      renderCounts();
      while (container.firstChild) {
        container.removeChild(container.firstChild);
      }
    }

    function clearReconnectTimer() {
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = 0;
      }
    }

    function levelClass(level) {
      switch (level) {
        case "error":
          return "error";
        case "warn":
          return "warn";
        case "debug":
          return "debug";
        default:
          return "info";
      }
    }

    function buildWsURL() {
      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
      const query = new URLSearchParams({
        component: componentSelect.value,
        level: levelSelect.value,
        q: searchInput.value,
      });
      return `${protocol}//${window.location.host}${basePath}/logs/stream?${query.toString()}`;
    }

    function buildDownloadURL() {
      const query = new URLSearchParams({ component: componentSelect.value });
      return `${basePath}/logs/download?${query.toString()}`;
    }

    function appendEntry(entry) {
      if (paused) return;

      const line = document.createElement("div");
      line.className = `log-line ${levelClass(entry.level)}`;

      const timestamp = entry.time
        ? new Date(entry.time).toISOString().replace("T", " ").replace("Z", "")
        : "";
      const level = (entry.level || "info").toUpperCase();
      line.textContent = `${timestamp} [${level}] ${entry.message || ""}`;

      container.appendChild(line);
      counts.buffered += 1;
      if (entry.level === "warn") counts.warn += 1;
      else if (entry.level === "error") counts.error += 1;
      else counts.info += 1;

      while (container.children.length > 2000) {
        const first = container.firstChild;
        if (first && first.classList) {
          counts.buffered = Math.max(0, counts.buffered - 1);
          if (first.classList.contains("warn")) counts.warn = Math.max(0, counts.warn - 1);
          else if (first.classList.contains("error")) counts.error = Math.max(0, counts.error - 1);
          else counts.info = Math.max(0, counts.info - 1);
        }
        container.removeChild(first);
      }
      renderCounts();

      if (autoScroll) {
        container.scrollTop = container.scrollHeight;
      }
    }

    function closeSocket() {
      if (!ws) return;
      const current = ws;
      ws = null;
      current.onclose = null;
      current.close();
    }

    function scheduleReconnect() {
      clearReconnectTimer();
      reconnectTimer = window.setTimeout(connect, 5000);
    }

    function connect() {
      clearReconnectTimer();
      closeSocket();
      statusEl.textContent = "Connecting…";

      ws = new WebSocket(buildWsURL());
      ws.onopen = () => {
        statusEl.textContent = `Connected — ${componentSelect.value} / ${levelSelect.value}`;
      };
      ws.onmessage = (event) => {
        try {
          appendEntry(JSON.parse(event.data));
        } catch (_) {}
      };
      ws.onerror = () => {
        statusEl.textContent = "Connection error";
      };
      ws.onclose = (event) => {
        ws = null;
        statusEl.textContent = `Disconnected (code ${event.code}). Reconnecting in 5s…`;
        scheduleReconnect();
      };
    }

    function reapply() {
      btnDownload.href = buildDownloadURL();
      resetBuffer();
      connect();
    }

    container.addEventListener("scroll", () => {
      const threshold = 40;
      autoScroll =
        container.scrollTop + container.clientHeight >= container.scrollHeight - threshold;
      renderAutoScrollButton();
    });

    btnPause.addEventListener("click", () => {
      paused = !paused;
      btnPause.textContent = paused ? "Resume" : "Pause";
      btnPause.classList.toggle("paused", paused);
    });

    componentSelect.addEventListener("change", reapply);
    levelSelect.addEventListener("change", reapply);
    levelButtons.forEach((button) => {
      button.addEventListener("click", () => {
        if (levelSelect.value === button.dataset.level) return;
        levelSelect.value = button.dataset.level;
        renderLevelButtons();
        reapply();
      });
    });
    searchInput.addEventListener("input", () => {
      window.clearTimeout(searchTimer);
      searchTimer = window.setTimeout(reapply, 400);
    });
    autoScrollButton.addEventListener("click", () => {
      autoScroll = !autoScroll;
      renderAutoScrollButton();
      if (autoScroll) {
        container.scrollTop = container.scrollHeight;
      }
    });

    window.addEventListener(
      "beforeunload",
      () => {
        clearReconnectTimer();
        closeSocket();
      },
      { once: true },
    );

    btnDownload.href = buildDownloadURL();
    renderCounts();
    renderLevelButtons();
    renderAutoScrollButton();
    connect();
  }

  function initUsersPage() {
    const root = document.querySelector("[data-users-page]");
    if (!root || root.dataset.usersInitialized === "true") return;

    const searchInput = root.querySelector('[data-users-role="search"]');
    const statusSelect = root.querySelector('[data-users-role="status"]');
    const statusButtons = Array.from(root.querySelectorAll("[data-users-status-value]"));
    const sortSelect = root.querySelector('[data-users-role="sort"]');
    const countEl = root.querySelector('[data-users-role="count"]');
    const table = root.querySelector(".users-table");
    const openCreateButton = document.querySelector("[data-users-open-create]");
    const createModal = document.querySelector("[data-users-modal]");
    const closeCreateButton = document.querySelector("[data-users-close-create]");
    const drawer = document.querySelector("[data-users-drawer]");
    const drawerScrim = document.querySelector("[data-users-drawer-scrim]");

    if (
      !searchInput ||
      !statusSelect ||
      !sortSelect ||
      !countEl ||
      !table ||
      !table.tBodies.length ||
      !openCreateButton ||
      !createModal ||
      !closeCreateButton ||
      !drawer ||
      !drawerScrim ||
      !statusButtons.length
    ) {
      return;
    }

    root.dataset.usersInitialized = "true";
    const tbody = table.tBodies[0];
    const rows = Array.from(tbody.querySelectorAll("[data-user-row]"));
    const selectAll = root.querySelector("[data-users-select-all]");
    const selectionEl = root.querySelector('[data-users-role="selection"]');
    const bulkForm = root.querySelector("[data-users-bulk-form]");
    const bulkIds = root.querySelector("[data-users-bulk-ids]");
    const sortHeaders = Array.from(root.querySelectorAll("[data-users-sort-key]"));
    const rowMenus = Array.from(root.querySelectorAll(".row-menu"));

    function rowCheckboxes() {
      return rows
        .map((row) => row.querySelector("[data-users-select]"))
        .filter(Boolean);
    }

    function updateSelection() {
      const boxes = rowCheckboxes();
      const visible = boxes.filter((b) => !b.closest("[data-user-row]").hidden);
      let checkedCount = 0;
      boxes.forEach((b) => {
        const row = b.closest("[data-user-row]");
        if (b.checked) {
          checkedCount += 1;
          row.dataset.selected = "true";
        } else {
          delete row.dataset.selected;
        }
      });
      if (selectionEl) {
        selectionEl.textContent = `${checkedCount} selected`;
      }
      if (bulkForm) {
        bulkForm.hidden = checkedCount === 0;
      }
      if (selectAll) {
        const visibleChecked = visible.filter((b) => b.checked).length;
        selectAll.checked = visible.length > 0 && visibleChecked === visible.length;
        selectAll.indeterminate = visibleChecked > 0 && visibleChecked < visible.length;
      }
    }

    function updateSortHeaders() {
      sortHeaders.forEach((header) => {
        header.classList.toggle("active", header.dataset.usersSortKey === sortSelect.value);
      });
    }

    function closeRowMenus(except) {
      rowMenus.forEach((menu) => {
        if (menu !== except) menu.removeAttribute("open");
      });
    }

    function asNumber(value) {
      const n = Number.parseInt(value || "0", 10);
      return Number.isNaN(n) ? 0 : n;
    }

    function matchesStatus(row, status) {
      if (status === "all") return true;
      if (status === "enabled") return row.dataset.enabled === "true";
      if (status === "disabled") return row.dataset.enabled !== "true";
      if (status === "suspended") return row.dataset.suspended === "true";
      return (row.dataset.connection || "").toLowerCase() === status;
    }

    function compareRows(a, b, sort) {
      if (sort === "created-desc") return asNumber(b.dataset.created) - asNumber(a.dataset.created);
      if (sort === "traffic-desc") return asNumber(b.dataset.traffic) - asNumber(a.dataset.traffic);
      if (sort === "connections-desc") return asNumber(b.dataset.connections) - asNumber(a.dataset.connections);
      return (a.dataset.label || "").localeCompare(b.dataset.label || "", undefined, { sensitivity: "base" });
    }

    function applyUsersState() {
      const query = searchInput.value.trim().toLowerCase();
      const status = statusSelect.value;
      const sort = sortSelect.value;
      const sortedRows = rows.slice().sort((a, b) => compareRows(a, b, sort));
      let visible = 0;

      sortedRows.forEach((row) => {
        const label = (row.dataset.label || "").toLowerCase();
        const show = (!query || label.includes(query)) && matchesStatus(row, status);
        row.hidden = !show;
        if (show) visible += 1;
        tbody.appendChild(row);
      });

      countEl.textContent = `${visible} user${visible === 1 ? "" : "s"}`;
      statusButtons.forEach((button) => {
        button.classList.toggle("active", button.dataset.usersStatusValue === status);
      });
      updateSortHeaders();
      updateSelection();
    }

    function toggleBodyLock() {
      const dialogOpen = !createModal.hidden || !drawer.hidden;
      document.body.classList.toggle("dialog-open", dialogOpen);
    }

    function openCreateModal() {
      createModal.hidden = false;
      toggleBodyLock();
      const input = createModal.querySelector('input[name="label"]');
      if (input) input.focus();
    }

    function closeCreateModal() {
      createModal.hidden = true;
      toggleBodyLock();
    }

    function setText(role, value) {
      const target = drawer.querySelector(`[data-users-detail="${role}"]`);
      if (target) target.textContent = value;
    }

    function setFormAction(role, value) {
      const form = drawer.querySelector(`[data-users-form="${role}"]`);
      if (form) form.action = value || "";
    }

    function openDrawer(row) {
      const quotaPercent = Math.max(0, Math.min(100, asNumber(row.dataset.quotaPercent)));
      const quotaBar = drawer.querySelector('[data-users-detail="quota-bar"]');
      const statusEl = drawer.querySelector('[data-users-detail="status"]');
      const connectionEl = drawer.querySelector('[data-users-detail="connection"]');
      const toggleButton = drawer.querySelector('[data-users-action-label="toggle"]');
      const suspendButton = drawer.querySelector('[data-users-action-label="suspend"]');
      const deleteButton = drawer.querySelector("[data-users-delete]");
      const quotaTone =
        row.dataset.suspended === "true" || quotaPercent >= 100
          ? "danger"
          : quotaPercent >= 80
            ? "warn"
            : "success";

      setText("label", row.dataset.label || "User");
      setText("meta", `${row.dataset.id || "—"} · created ${row.dataset.createdLabel || "—"}`);
      setText("status", row.dataset.statusLabel || "unknown");
      setText("connection", row.dataset.connection || "not connected");
      setText("quota-used", row.dataset.quotaUsed || "0 B");
      setText("quota-limit", row.dataset.quotaLimit || "Unlimited");
      setText("quota-reset", row.dataset.quotaReset || "—");
      setText("download", row.dataset.download || "0 B");
      setText("upload", row.dataset.upload || "0 B");
      setText("connections", row.dataset.connections || "0");
      setText("created", row.dataset.createdLabel || "—");
      setText("quota-period", row.dataset.quotaPeriod || "—");
      setText("total", row.dataset.total || "0 B");

      if (quotaBar) {
        quotaBar.value = quotaPercent;
        quotaBar.dataset.tone = quotaTone;
      }
      if (statusEl) {
        statusEl.dataset.tone = row.dataset.enabled === "true" ? "success" : "danger";
      }
      if (connectionEl) {
        const connection = (row.dataset.connection || "").toLowerCase();
        connectionEl.dataset.tone =
          connection === "online" ? "success" : connection === "offline" ? "warn" : "accent";
      }
      if (toggleButton) {
        toggleButton.textContent = row.dataset.toggleLabel || "Toggle";
      }
      if (suspendButton) {
        suspendButton.textContent = row.dataset.suspendLabel || "Suspend";
      }
      if (deleteButton) {
        deleteButton.dataset.label = row.dataset.label || "user";
      }

      drawer.dataset.linkUrl = row.dataset.linkUrl || "";
      drawer.dataset.qrUrl = row.dataset.qrUrl || "";
      const linkRow = drawer.querySelector("[data-users-link-row]");
      if (linkRow) {
        linkRow.hidden = true;
        const linkVal = linkRow.querySelector("[data-users-detail='link']");
        if (linkVal) linkVal.textContent = "";
      }
      const qrFrame = drawer.querySelector("[data-users-qr-frame]");
      if (qrFrame) {
        qrFrame.hidden = true;
        const qrImg = qrFrame.querySelector("[data-users-qr-img]");
        if (qrImg) qrImg.src = "";
      }

      setFormAction("toggle", row.dataset.toggleUrl);
      setFormAction("rotate", row.dataset.rotateUrl);
      setFormAction("suspend", row.dataset.suspendUrl);
      setFormAction("reset", row.dataset.resetUrl);
      setFormAction("quota", row.dataset.quotaUrl);
      setFormAction("delete", row.dataset.deleteUrl);

      closeRowMenus();
      drawer.hidden = false;
      drawerScrim.hidden = false;
      toggleBodyLock();
    }

    function closeDrawer() {
      drawer.hidden = true;
      drawerScrim.hidden = true;
      toggleBodyLock();
    }

    searchInput.addEventListener("input", applyUsersState);
    statusSelect.addEventListener("change", applyUsersState);
    statusButtons.forEach((button) => {
      button.addEventListener("click", () => {
        if (statusSelect.value === button.dataset.usersStatusValue) return;
        statusSelect.value = button.dataset.usersStatusValue;
        applyUsersState();
      });
    });
    sortSelect.addEventListener("change", applyUsersState);
    sortHeaders.forEach((header) => {
      header.addEventListener("click", () => {
        const key = header.dataset.usersSortKey;
        if (sortSelect.value !== key) sortSelect.value = key;
        applyUsersState();
      });
    });
    if (selectAll) {
      selectAll.addEventListener("change", () => {
        rowCheckboxes().forEach((box) => {
          if (!box.closest("[data-user-row]").hidden) box.checked = selectAll.checked;
        });
        updateSelection();
      });
    }
    rowCheckboxes().forEach((box) => box.addEventListener("change", updateSelection));
    rowMenus.forEach((menu) => {
      const summary = menu.querySelector("summary");
      if (summary) {
        summary.addEventListener("click", () => {
          if (!menu.open) closeRowMenus(menu);
        });
      }
    });
    document.addEventListener("click", (event) => {
      rowMenus.forEach((menu) => {
        if (menu.open && !menu.contains(event.target)) menu.removeAttribute("open");
      });
    });
    openCreateButton.addEventListener("click", openCreateModal);
    closeCreateButton.addEventListener("click", closeCreateModal);
    createModal.addEventListener("click", (event) => {
      if (event.target === createModal) closeCreateModal();
    });
    drawerScrim.addEventListener("click", closeDrawer);
    drawer.querySelectorAll("[data-users-close-drawer]").forEach((button) => {
      button.addEventListener("click", closeDrawer);
    });
    drawer.querySelectorAll('[data-users-form="delete"]').forEach((form) => {
      form.addEventListener("submit", (event) => {
        const label = drawer.querySelector("[data-users-delete]")?.dataset.label || "user";
        if (!window.confirm(`Delete ${label}?`)) {
          event.preventDefault();
        }
      });
    });
    rows.forEach((row) => {
      row.querySelectorAll("[data-user-open]").forEach((button) => {
        button.addEventListener("click", () => openDrawer(row));
      });
    });
    const revealButton = drawer.querySelector("[data-users-reveal-link]");
    if (revealButton) {
      revealButton.addEventListener("click", async () => {
        const linkUrl = drawer.dataset.linkUrl;
        if (!linkUrl) return;
        const csrfToken = readCookie("csrf_token");
        try {
          const resp = await fetch(linkUrl, {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: "_csrf=" + encodeURIComponent(csrfToken),
          });
          if (!resp.ok) return;
          const text = await resp.text();
          const linkRow = drawer.querySelector("[data-users-link-row]");
          const linkVal = drawer.querySelector("[data-users-detail='link']");
          if (linkRow && linkVal) {
            linkVal.textContent = text;
            linkRow.hidden = false;
            initCopyButtons();
          }
        } catch (_) {}
      });
    }
    const revealQRButton = drawer.querySelector("[data-users-reveal-qr]");
    if (revealQRButton) {
      revealQRButton.addEventListener("click", async () => {
        const qrUrl = drawer.dataset.qrUrl;
        if (!qrUrl) return;
        const csrfToken = readCookie("csrf_token");
        try {
          const resp = await fetch(qrUrl, {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: "_csrf=" + encodeURIComponent(csrfToken),
          });
          if (!resp.ok) return;
          const b64 = await resp.text();
          const qrFrame = drawer.querySelector("[data-users-qr-frame]");
          const qrImg = drawer.querySelector("[data-users-qr-img]");
          if (qrFrame && qrImg) {
            qrImg.src = "data:image/png;base64," + b64;
            qrFrame.hidden = false;
          }
        } catch (_) {}
      });
    }
    document.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      if (!createModal.hidden) closeCreateModal();
      if (!drawer.hidden) closeDrawer();
    });
    if (bulkForm) {
      bulkForm.addEventListener("submit", (event) => {
        const boxes = rowCheckboxes();
        const checked = boxes.filter((b) => b.checked);
        if (checked.length === 0) {
          event.preventDefault();
          return;
        }
        const action = event.submitter?.dataset.usersBulkAction;
        if (action === "delete") {
          if (!window.confirm(`Delete ${checked.length} selected user(s)? This cannot be undone.`)) {
            event.preventDefault();
            return;
          }
        }
        if (bulkIds) {
          while (bulkIds.firstChild) {
            bulkIds.removeChild(bulkIds.firstChild);
          }
          checked.forEach((box) => {
            const row = box.closest("[data-user-row]");
            const id = row?.dataset.id;
            if (id) {
              const input = document.createElement("input");
              input.type = "hidden";
              input.name = "id";
              input.value = id;
              bulkIds.appendChild(input);
            }
          });
        }
      });
    }
    applyUsersState();
  }

  function initBridgePage() {
    const root = document.querySelector("[data-bridge-page]");
    if (!root || root.dataset.bridgeInitialized === "true") return;

    const openButton = document.querySelector("[data-bridge-open-add]");
    const modal = document.querySelector("[data-bridge-modal]");
    const closeButton = document.querySelector("[data-bridge-close-add]");
    if (!openButton || !modal || !closeButton) return;

    root.dataset.bridgeInitialized = "true";

    function syncBodyLock() {
      const otherDialogOpen =
        document.querySelector("[data-users-modal]:not([hidden])") ||
        document.querySelector("[data-users-drawer]:not([hidden])");
      document.body.classList.toggle("dialog-open", !modal.hidden || !!otherDialogOpen);
    }

    function openModal() {
      modal.hidden = false;
      syncBodyLock();
      const input = modal.querySelector('input[name="share_url"]');
      if (input) input.focus();
    }

    function closeModal() {
      modal.hidden = true;
      syncBodyLock();
    }

    openButton.addEventListener("click", openModal);
    closeButton.addEventListener("click", closeModal);
    modal.addEventListener("click", (event) => {
      if (event.target === modal) closeModal();
    });

    root.querySelectorAll('form[action*="/delete"]').forEach((form) => {
      form.addEventListener("submit", (event) => {
        const row = form.closest("tr");
        const label = row?.querySelector("strong")?.textContent?.trim() || "node";
        if (!window.confirm(`Delete node ${label}?`)) {
          event.preventDefault();
        }
      });
    });

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && !modal.hidden) closeModal();
    });
  }

  function initPasswordPage() {
    const root = document.querySelector("[data-password-page]");
    if (!root || root.dataset.passwordInitialized === "true") return;

    const toggles = Array.from(root.querySelectorAll('[data-password-role="toggle"]'));
    const strengthSource = root.querySelector("[data-password-strength-source]");
    const confirmField = root.querySelector("[data-password-confirm]");
    const strengthMeter = root.querySelector('[data-password-role="strength-meter"] span');
    const strengthTone = root.querySelector('[data-password-role="strength-meter"]');
    const strengthNote = root.querySelector('[data-password-role="strength-note"]');
    const matchNote = root.querySelector('[data-password-role="match-note"]');

    if (!toggles.length || !strengthSource || !confirmField || !strengthMeter || !strengthTone || !strengthNote || !matchNote) {
      return;
    }

    root.dataset.passwordInitialized = "true";

    function scorePassword(value) {
      let score = 0;
      if (value.length >= 16) score += 50;
      else score += Math.min(40, value.length * 2);
      if (/[a-zA-Z]/.test(value)) score += 20;
      if (/\d/.test(value)) score += 20;
      if (/[^a-zA-Z0-9]/.test(value)) score += 10;
      return Math.min(100, score);
    }

    function renderStrength() {
      const value = strengthSource.value;
      const score = scorePassword(value);
      let tone = "danger";
      let note = "Minimum 16 characters, must contain letters and digits.";

      if (score >= 85) {
        tone = "success";
        note = "Strong password.";
      } else if (score >= 65) {
        tone = "warn";
        note = "Acceptable, but a longer passphrase is better.";
      } else if (value.length > 0) {
        tone = "danger";
        note = "Too weak. Increase length and mix letters with digits.";
      }

      strengthMeter.style.width = `${score}%`;
      strengthTone.dataset.tone = tone;
      strengthNote.textContent = note;
    }

    function renderMatch() {
      if (!confirmField.value) {
        matchNote.textContent = "Repeat the new password exactly.";
        matchNote.classList.remove("success", "error");
        return;
      }
      if (confirmField.value === strengthSource.value) {
        matchNote.textContent = "Passwords match.";
        matchNote.classList.add("success");
        matchNote.classList.remove("error");
        return;
      }
      matchNote.textContent = "Passwords do not match.";
      matchNote.classList.add("error");
      matchNote.classList.remove("success");
    }

    toggles.forEach((toggle) => {
      toggle.addEventListener("click", () => {
        const input = root.querySelector(`#${toggle.dataset.passwordTarget}`);
        if (!input) return;
        const nextType = input.type === "password" ? "text" : "password";
        input.type = nextType;
        toggle.textContent = nextType === "password" ? "Show" : "Hide";
      });
    });

    strengthSource.addEventListener("input", () => {
      renderStrength();
      renderMatch();
    });
    confirmField.addEventListener("input", renderMatch);
    renderStrength();
    renderMatch();
  }

  // initCopyButtons enables every [data-copy] button on the page and wires up
  // clipboard copy on click. The button must sit inside a .copy-row container
  // alongside a .val element whose text will be copied. Buttons start as
  // disabled in HTML so they are inert when JS is unavailable.
  function initCopyButtons() {
    document.querySelectorAll("[data-copy]").forEach((btn) => {
      btn.disabled = false;
      // Guard against double-binding when called again after htmx swaps.
      if (btn.dataset.copyInit === "true") return;
      btn.dataset.copyInit = "true";
      btn.addEventListener("click", () => {
        const row = btn.closest(".copy-row");
        if (!row) return;
        const val = row.querySelector(".val");
        if (!val) return;
        const text = val.textContent.trim();
        // Clone child nodes so we can restore the icon after the feedback label.
        const origNodes = Array.from(btn.childNodes).map((n) => n.cloneNode(true));
        const showFeedback = () => {
          btn.textContent = "Copied!";
          setTimeout(() => {
            btn.textContent = "";
            origNodes.forEach((n) => btn.appendChild(n));
          }, 1500);
        };
        const fallback = () => {
          const range = document.createRange();
          range.selectNodeContents(val);
          const sel = window.getSelection();
          if (sel) { sel.removeAllRanges(); sel.addRange(range); }
        };
        if (navigator.clipboard) {
          navigator.clipboard.writeText(text).then(showFeedback).catch(fallback);
        } else {
          fallback();
        }
      });
    });
  }

  function initStubUpload() {
    const form = document.querySelector("[data-stub-upload]");
    if (!form || form.dataset.stubUploadInitialized === "true") return;
    const zone = form.querySelector("[data-stub-dropzone]");
    const input = form.querySelector("[data-stub-file]");
    const filename = form.querySelector("[data-stub-filename]");
    if (!zone || !input || !filename) return;

    form.dataset.stubUploadInitialized = "true";

    function showName() {
      const file = input.files && input.files[0];
      if (file) {
        filename.textContent = file.name;
        filename.hidden = false;
      } else {
        filename.hidden = true;
      }
    }

    input.addEventListener("change", showName);
    ["dragenter", "dragover"].forEach((evt) =>
      zone.addEventListener(evt, (e) => {
        e.preventDefault();
        zone.classList.add("is-dragover");
      }),
    );
    ["dragleave", "dragend", "drop"].forEach((evt) =>
      zone.addEventListener(evt, () => zone.classList.remove("is-dragover")),
    );
    zone.addEventListener("drop", (e) => {
      e.preventDefault();
      if (e.dataTransfer && e.dataTransfer.files.length) {
        input.files = e.dataTransfer.files;
        showName();
      }
    });
  }

  function initStubRemoteSearch() {
    const input = document.querySelector("[data-stub-remote-search]");
    if (!input || input.dataset.stubSearchInitialized === "true") return;
    input.dataset.stubSearchInitialized = "true";

    const cards = Array.from(document.querySelectorAll("[data-stub-card]"));

    input.addEventListener("input", () => {
      const q = input.value.trim().toLowerCase();
      cards.forEach((card) => {
        const name = (card.dataset.stubName || "").toLowerCase();
        card.hidden = q !== "" && !name.includes(q);
      });
    });
  }

  function initPanelPage() {
    fillCSRF();
    initLogsPage();
    initUsersPage();
    initBridgePage();
    initPasswordPage();
    initStubUpload();
    initStubRemoteSearch();
    initCopyButtons();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initPanelPage);
  } else {
    initPanelPage();
  }

  document.body.addEventListener("htmx:afterSwap", () => {
    fillCSRF();
    initLogsPage();
    initUsersPage();
    initBridgePage();
    initPasswordPage();
    initStubUpload();
    initStubRemoteSearch();
    initCopyButtons();
  });
})();
