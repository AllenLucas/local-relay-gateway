document.addEventListener("DOMContentLoaded", () => {
  const active = document.querySelector(`a[href="${window.location.pathname}"]`);
  if (active) active.style.fontWeight = "700";

  document.querySelectorAll("[data-copy-target]").forEach((button) => {
    button.addEventListener("click", async () => {
      const target = document.getElementById(button.dataset.copyTarget);
      if (!target) return;
      const value = target.value || target.textContent || "";
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(value);
      } else {
        target.select();
        document.execCommand("copy");
      }
      button.textContent = "Copied";
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
