document.addEventListener("DOMContentLoaded", () => {
  const active = document.querySelector(`a[href="${window.location.pathname}"]`);
  if (active) active.style.fontWeight = "700";

  document.querySelectorAll("[data-copy-target]").forEach((button) => {
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
      button.textContent = copied ? "Copied" : "Copy failed";
      window.setTimeout(() => {
        button.textContent = "Copy";
      }, 1200);
    });
  });

  document.querySelectorAll("[data-toggle-password]").forEach((button) => {
    button.addEventListener("click", () => {
      const target = document.getElementById(button.dataset.togglePassword);
      if (!target) return;
      const showing = target.type === "text";
      target.type = showing ? "password" : "text";
      button.textContent = showing ? "Show" : "Hide";
    });
  });
});
