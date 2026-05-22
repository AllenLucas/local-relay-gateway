document.addEventListener("DOMContentLoaded", () => {
  const path = window.location.pathname;
  let best = null;
  let bestLen = 0;
  document.querySelectorAll("nav.tabs a").forEach((link) => {
    const href = link.getAttribute("href");
    if (!href) return;
    if (path === href || path.startsWith(href + "/")) {
      if (href.length > bestLen) {
        best = link;
        bestLen = href.length;
      }
    }
  });
  if (best) best.classList.add("active");

  document.querySelectorAll("[data-copy-target]").forEach((button) => {
    const original = button.textContent;
    button.addEventListener("click", async () => {
      const target = document.getElementById(button.dataset.copyTarget);
      if (!target) return;
      const value = target.value || target.textContent || "";
      let copied = false;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        try {
          await navigator.clipboard.writeText(value);
          copied = true;
        } catch (_) {
          copied = false;
        }
      }
      if (!copied) {
        target.select();
        copied = document.execCommand("copy");
      }
      button.textContent = copied ? "已复制" : "复制失败";
      window.setTimeout(() => {
        button.textContent = original;
      }, 1200);
    });
  });

  document.querySelectorAll("form[data-confirm]").forEach((form) => {
    form.addEventListener("submit", (event) => {
      if (!window.confirm(form.dataset.confirm)) {
        event.preventDefault();
      }
    });
  });

  document.querySelectorAll("button[data-confirm]").forEach((button) => {
    button.addEventListener("click", (event) => {
      if (!window.confirm(button.dataset.confirm)) {
        event.preventDefault();
      }
    });
  });

  document.querySelectorAll("[data-toggle-password]").forEach((button) => {
    button.addEventListener("click", () => {
      const target = document.getElementById(button.dataset.togglePassword);
      if (!target) return;
      const showing = target.type === "text";
      target.type = showing ? "password" : "text";
      button.textContent = showing ? "显示" : "隐藏";
    });
  });

  const localTimeFormatter = new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  document.querySelectorAll("time[data-local-time]").forEach((node) => {
    const raw = node.getAttribute("datetime");
    if (!raw) return;
    const parsed = new Date(raw);
    if (Number.isNaN(parsed.getTime())) return;
    node.textContent = localTimeFormatter.format(parsed);
    node.title = raw;
  });
});
