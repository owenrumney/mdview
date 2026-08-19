(function () {
  var overlay = null;
  var content = null;

  function ensureOverlay() {
    if (overlay) return;
    overlay = document.createElement('div');
    overlay.className = 'mdview-zoom-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    content = document.createElement('div');
    content.className = 'mdview-zoom-content';
    overlay.appendChild(content);
    overlay.addEventListener('click', close);
    document.body.appendChild(overlay);
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') close();
    });
  }

  function open(node) {
    ensureOverlay();
    content.innerHTML = '';
    var clone = node.cloneNode(true);
    if (clone.tagName && clone.tagName.toLowerCase() === 'svg') {
      clone.removeAttribute('width');
      clone.removeAttribute('height');
      clone.style.maxWidth = 'unset';
      clone.style.maxHeight = 'unset';
    }
    content.appendChild(clone);
    overlay.classList.add('open');
  }

  function close() {
    if (!overlay) return;
    overlay.classList.remove('open');
    if (content) content.innerHTML = '';
  }

  document.addEventListener('click', function (e) {
    var target = e.target;
    if (!target || !target.closest) return;
    var img = target.closest('main.markdown-body img');
    if (img && !img.closest('a')) {
      e.preventDefault();
      open(img);
      return;
    }
    var svg = target.closest('main.markdown-body pre.mermaid svg');
    if (svg) {
      e.preventDefault();
      open(svg);
      return;
    }
  });
})();
