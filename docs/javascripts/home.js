const giSearch = document.querySelector('[data-gi-search]');
if (giSearch) {
  giSearch.addEventListener('click', () => {
    const toggle = document.getElementById('__search');
    const input = document.querySelector('[data-md-component="search-query"]');
    if (toggle && input) {
      toggle.checked = true;
      toggle.dispatchEvent(new Event('change', { bubbles: true }));
      input.focus();
    }
  });
}
