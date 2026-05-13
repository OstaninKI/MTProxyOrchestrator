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

  document.addEventListener("DOMContentLoaded", fillCSRF);
  document.body.addEventListener("htmx:afterSwap", fillCSRF);
})();
