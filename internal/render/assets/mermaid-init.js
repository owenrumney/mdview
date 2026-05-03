(function () {
  function init() {
    if (!window.mermaid) return;
    var theme = window.MDVIEW_MERMAID_THEME || 'dark';
    var security = window.MDVIEW_MERMAID_SECURITY || 'strict';
    mermaid.initialize({
      startOnLoad: false,
      theme: theme,
      securityLevel: security
    });
    var blocks = Array.prototype.slice.call(document.querySelectorAll('pre.mermaid'));
    blocks.forEach(function (el) {
      var source = el.textContent;
      Promise.resolve()
        .then(function () { return mermaid.run({ nodes: [el] }); })
        .catch(function (err) {
          var box = document.createElement('div');
          box.className = 'mermaid-error';
          var title = document.createElement('strong');
          title.textContent = 'Mermaid render failed';
          var msg = document.createElement('pre');
          msg.className = 'mermaid-error-msg';
          msg.textContent = (err && err.message) ? err.message : String(err);
          var src = document.createElement('pre');
          src.className = 'mermaid-error-src';
          src.textContent = source.replace(/^\s+|\s+$/g, '');
          box.appendChild(title);
          box.appendChild(msg);
          box.appendChild(src);
          el.replaceWith(box);
        });
    });
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
