const form = document.querySelector('[data-gi-search]');
const input = form && form.querySelector('input');
const results = form && form.querySelector('.gi-search__results');
const query = document.querySelector('[data-md-component="search-query"]');
const result = document.querySelector('[data-md-component="search-result"]');

if (form && input && results && query && result) {
  const ask = () => {
    query.value = input.value;
    query.dispatchEvent(new Event('keyup'));
  };

  const show = () => {
    if (query.value !== input.value || !input.value.trim()) {
      results.hidden = true;
      results.replaceChildren();
      return;
    }
    const links = result.querySelectorAll('.md-search-result__link');
    const list = document.createElement('ul');
    const pages = new Set();
    for (const link of links) {
      const page = link.href.split('#')[0];
      if (pages.has(page)) continue;
      pages.add(page);
      if (pages.size > 8) break;
      const title = link.querySelector('h1, h2');
      const item = document.createElement('li');
      const anchor = document.createElement('a');
      anchor.href = link.href;
      const heading = document.createElement('strong');
      heading.textContent = title ? title.textContent : link.textContent;
      anchor.append(heading);
      const article = link.querySelector('article');
      let teaser = '';
      if (article) {
        const copy = article.cloneNode(true);
        copy.querySelectorAll('h1, h2, .md-search-result__terms, .md-search-result__icon').forEach((node) => node.remove());
        copy.querySelectorAll('p, pre, li, td, th').forEach((node) => node.append(' '));
        teaser = copy.textContent.replace(/\s+/g, ' ').trim();
        if (teaser.length > 140) teaser = teaser.slice(0, 140).replace(/\s+\S*$/, '') + '...';
      }
      if (teaser) {
        const note = document.createElement('span');
        note.textContent = teaser;
        anchor.append(note);
      }
      item.append(anchor);
      list.append(item);
    }
    if (!list.childElementCount) {
      const empty = document.createElement('p');
      empty.textContent = 'No matching pages';
      results.replaceChildren(empty);
    } else {
      results.replaceChildren(list);
    }
    results.hidden = false;
  };

  input.addEventListener('focus', show);
  input.addEventListener('input', () => {
    results.hidden = true;
    ask();
  });
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const first = results.querySelector('a');
    if (first) first.click();
  });
  input.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowDown') {
      const first = results.querySelector('a');
      if (first) {
        event.preventDefault();
        first.focus();
      }
    }
  });
  form.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      input.focus({ preventScroll: true });
      results.hidden = true;
    } else if (results.contains(event.target) && ['ArrowDown', 'ArrowUp'].includes(event.key)) {
      event.preventDefault();
      const links = [...results.querySelectorAll('a')];
      const index = links.indexOf(document.activeElement);
      if (event.key === 'ArrowUp' && index === 0) input.focus();
      else links[index + (event.key === 'ArrowDown' ? 1 : -1)]?.focus();
    }
  });
  document.addEventListener('click', (event) => {
    if (!form.contains(event.target)) results.hidden = true;
  });
  document.addEventListener('keydown', (event) => {
    if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return;
    const target = event.target;
    if (target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    input.focus();
  }, true);
  new MutationObserver(show).observe(result, { childList: true, subtree: true, characterData: true });
}
