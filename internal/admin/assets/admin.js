document.addEventListener("DOMContentLoaded", () => {
  const active = document.querySelector(`a[href="${window.location.pathname}"]`);
  if (active) active.style.fontWeight = "700";
});
