/* Mockup chrome only (theme toggle); not part of the design. */
(function () {
  var r = document.documentElement, p = new URLSearchParams(location.search).get("theme");
  if (p === "light" || p === "dark") r.dataset.theme = p;
  document.addEventListener("DOMContentLoaded", function () {
    var b = document.querySelector(".mock-theme");
    if (!b) return;
    function s() { b.textContent = r.dataset.theme === "light" ? "Light" : "Dark"; }
    s();
    b.onclick = function () { r.dataset.theme = r.dataset.theme === "light" ? "dark" : "light"; s(); };
  });
})();
