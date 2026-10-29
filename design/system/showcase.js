/* Renders the showcase specimens from tokens.js and the component templates below. Not part of the system. */
const T = window.MH_TOKENS;
const $ = (sel) => document.querySelector(sel);
const icon = (name, cls = '') => `<svg class="icon ${cls}" aria-hidden="true"><use href="#i-${name}"/></svg>`;

/* ---------- Component templates ---------- */
function ratingDisplay(avg, count) {
  if (!count) return `<span class="rating"><span class="rating__count">No Reviews yet</span></span>`;
  const reviews = `${count} ${count === 1 ? 'Review' : 'Reviews'}`;
  const segs = [1, 2, 3, 4, 5].map(i => `<span style="--fill:${Math.round(Math.max(0, Math.min(1, avg - i + 1)) * 100)}%"></span>`).join('');
  return `<span class="rating" role="img" aria-label="Rated ${avg.toFixed(1)} out of 5 from ${reviews}">
    <span class="rating__value" aria-hidden="true">${avg.toFixed(1)}</span>
    <span aria-hidden="true"><span class="rating__meter">${segs}</span><span class="rating__count num">${reviews}</span></span>
  </span>`;
}

function rateInput({ value = 0, preview = 0, focus = 0, pressed = 0, disabled = false, error = '', live = false } = {}) {
  const stars = [1, 2, 3, 4, 5].map(i => {
    const cls = [i <= value && 'is-on', i <= preview && 'is-preview', i === focus && 'is-focus', i === pressed && 'is-pressed'].filter(Boolean).join(' ');
    const tab = (value ? i === value : i === 1) ? 0 : -1;
    return `<button type="button" role="radio" class="rate__star ${cls}" aria-checked="${i === value}" aria-label="${i} of 5" tabindex="${tab}" ${disabled ? 'disabled' : ''}>${icon('star')}</button>`;
  }).join('');
  return `<div class="rate-field ${error ? 'rate-field--error' : ''} ${disabled ? 'rate-field--disabled' : ''}">
    <div class="field__label">Your rating <span class="muted">Required</span></div>
    <div class="rate-field__row"><div class="rate" role="radiogroup" aria-label="Your rating, 1 to 5" aria-required="true" ${error ? 'aria-invalid="true"' : ''} ${live ? 'data-live' : ''}>${stars}</div>
      <span class="rate__value" aria-hidden="true">${value ? `${value} of 5` : 'Not rated'}</span></div>
    ${error ? `<span class="field__error">${icon('circle-alert', 'icon--sm')}${error}</span>` : '<span class="field__help">Whole numbers from 1 to 5. 5 is best.</span>'}
  </div>`;
}

const starsReadOnly = (n) => `<span class="stars-ro" role="img" aria-label="Rated ${n} out of 5">${[1, 2, 3, 4, 5].map(i => icon('star', i <= n ? 'is-on' : '')).join('')}</span>`;
const verifiedBadge = () => `<span class="verified" title="A moderator accepted this Venue for the public catalogue">${icon('check')}Verified Venue</span>`;
const genreTags = (genres) => `<div class="tags">${genres.map(g => `<span class="tag">${g}</span>`).join('')}</div>`;
const chip = (label, { selected = false, state = '', disabled = false } = {}) =>
  `<button type="button" class="chip ${state}" aria-pressed="${selected}" ${disabled ? 'disabled' : ''}>${icon('check')}${label}</button>`;

const initials = (name) => name.split(/\s+/).filter(w => /^\p{L}/u.test(w)).slice(0, 2).map(w => w[0]).join('').toUpperCase();
const identity = (u, size = 'compact', photo = false) => `<div class="identity identity--${size}">
    <span class="avatar ${photo ? 'avatar--photo' : ''}" aria-hidden="true">${photo ? 'Photo' : initials(u.name)}</span>
    <span class="identity__text"><span class="identity__name" title="${u.name}">${u.name}</span><span class="identity__username">@${u.username}</span></span>
  </div>`;

const EXTRAS = { darts: ['target', 'Darts'], pool: ['circle-dot', 'Pool'], live: ['music', 'Live music'], outdoor: ['trees', 'Outdoor seating'], kitchen: ['utensils', 'Kitchen'] };
function venueCard(v, state = '') {
  return `<article class="venue-card ${state}">
    ${v.photo ? `<div class="venue-card__photo"><span class="t-label">Photo</span><span class="t-caption">Venue interior · 16:9</span></div>` : ''}
    <div class="venue-card__body">
      <div class="venue-card__top">${v.verified ? verifiedBadge() : '<span></span>'}<span class="distance">${v.distance}</span></div>
      <h3 class="venue-card__name"><a class="venue-card__link" href="#venue-card">${v.name}</a></h3>
      <div class="venue-card__meta"><span>${v.type}</span><span>${icon('map-pin', 'icon--sm')}${v.city}, ${v.country}</span></div>
      ${genreTags(v.genres)}
      ${v.extras ? `<div class="venue-card__extras">${v.extras.map(e => `<span>${icon(EXTRAS[e][0], 'icon--sm')}${EXTRAS[e][1]}</span>`).join('')}</div>` : ''}
      <div class="venue-card__foot">${ratingDisplay(v.avg, v.count)}</div>
    </div>
  </article>`;
}

function review(r) {
  const body = r.removed
    ? `<p class="review__removed">${icon('message-square-off', 'icon--sm')}Commentary removed by a moderator.</p>`
    : r.text ? `<p>${r.text}</p>` : '';
  return `<article class="review">
    <div class="review__head">${identity(r.user)}<time class="review__date num" datetime="${r.iso}">${r.date}</time></div>
    ${starsReadOnly(r.rating)}
    ${body}
  </article>`;
}

const checkinDone = (time) => `${icon('check')}<span style="display:grid;gap:4px;text-align:left">Checked in<span class="btn__sub">Today, ${time}</span></span>`;

/* ---------- Sample content (illustrative, not real data) ---------- */
const AMBOSS = { name: 'Zum Schwarzen Amboss', type: 'Pub', city: 'Düsseldorf', country: 'Germany', genres: ['Classic hard rock', 'Thrash', 'Stoner'], extras: ['darts', 'live', 'kitchen'], avg: 4.3, count: 128, distance: '1.2 km', verified: true, photo: true };
const SAITE = { name: 'Rostige Saite', type: 'Club', city: 'Essen', country: 'Germany', genres: ['Death metal', 'Black metal', 'Doom'], extras: ['live', 'outdoor'], avg: 4.6, count: 37, distance: '18 km', verified: true, photo: false };
const KAI = { name: 'Kai Hansen', username: 'kai_h' };
const MIRA = { name: 'Mira Okafor', username: 'mira_doom' };
const LONG = { name: 'Kai „Der Amboss“ Hansen aus der Düsseldorfer Altstadt', username: 'der_amboss_aus_der_altstadt' };
const GENRES = ['Classic hard rock', 'Death metal', 'Black metal', 'Folk metal', 'Doom', 'Thrash', 'Punk', 'Stoner', 'Heavy metal', 'Grindcore'];

/* ---------- Foundations ---------- */
$('#color-table tbody').innerHTML = Object.entries(T.color).map(([k, v]) =>
  `<tr><td><code>--color-${k}</code></td><td><span class="swatch" style="background:${v.dark}"></span><code>${v.dark}</code></td><td><span class="swatch" style="background:${v.light}"></span><code>${v.light}</code></td><td><code>${v.compose}</code></td></tr>`).join('');

const byPair = {};
for (const m of T.measured) (byPair[m.fg + '|' + m.bg] ??= { ...m })[m.mode] = m.ratio;
$('#contrast-table tbody').innerHTML = Object.values(byPair).map(p => {
  const cell = (r) => `<span class="num">${r.toFixed(2)}:1</span> <span class="${r >= p.min ? 'pass' : 'fail'}">${r >= p.min ? 'Pass' : 'Fail'}</span>`;
  return `<tr><td><code>${p.fg}</code></td><td><code>${p.bg}</code></td><td class="num">${p.min}:1 <span class="muted t-caption">${p.min === 3 ? 'UI part' : 'text'}</span></td><td>${cell(p.dark)}</td><td>${cell(p.light)}</td></tr>`;
}).join('');

const SAMPLE = { display: 'Rock bars in Düsseldorf', title: 'Zum Schwarzen Amboss', heading: 'Recent Check-ins', body: 'Cold Altbier, a dartboard by the stage and Motörhead on the jukebox every night.', label: 'Venue type', caption: 'Checked in 12 Oct 2026 · 21:14' };
$('#type-scale').innerHTML = Object.entries(T.type).map(([k, v]) =>
  `<div class="type-row"><div><div class="t-label">${k}</div><div class="code">${v.size}/${v.lineHeight} · ${v.weight} · ${T.font[v.font].family}</div></div><div class="t-${k}">${SAMPLE[k]}</div></div>`).join('');

$('#space-scale').innerHTML = Object.entries(T.space).map(([k, v]) =>
  `<div class="space-row"><code>--space-${k}</code><span class="num muted t-caption">${v} px</span><div class="space-bar" style="width:${v}px"></div></div>`).join('');
$('#radius-scale').innerHTML = Object.entries(T.radius).map(([k, v]) =>
  `<div><div class="radius-box" style="border-radius:${Math.min(v, 32)}px"></div><div class="t-caption" style="margin-top:6px"><code>--radius-${k}</code> <span class="num muted">${v === 999 ? 'pill' : v + ' px'}</span></div></div>`).join('');
$('#border-scale').innerHTML = Object.entries(T.border).map(([k, v]) =>
  `<div class="row" style="align-items:center"><div class="border-line" style="height:${v}px"></div><code>--border-${k}</code><span class="num muted t-caption">${v} px</span></div>`).join('');

$('#motion-list').innerHTML = Object.entries(T.motion.easing).flatMap(([e, curve]) =>
  Object.entries(T.motion.duration).filter(([d]) => e === 'standard' || d === 'slow').map(([d, ms]) =>
    `<button type="button" class="motion-item">
      <span><span class="t-label">${d} · ${e}</span><br><span class="code">${ms} ms · cubic-bezier(${curve.join(', ')})</span></span>
      <span class="motion-track"><span class="motion-dot" style="transition:left var(--duration-${d}) var(--easing-${e})"></span></span>
    </button>`)).join('');
$('#motion-list').addEventListener('click', (ev) => ev.target.closest('.motion-item')?.querySelector('.motion-track').classList.toggle('motion-go'));

const ICON_USES = { check: 'Verified Venue, done', 'map-pin': 'Location', 'map-pin-check': 'Check in', star: 'Rating', target: 'Darts', 'circle-dot': 'Pool', music: 'Live music', trees: 'Outdoor seating', utensils: 'Kitchen', 'circle-alert': 'Error', 'refresh-cw': 'Try again', 'message-square-off': 'Commentary removed', 'map-pinned': 'Empty list', search: 'No matches', mail: 'Email', lock: 'Password', plus: 'Submit a Venue', 'trash-2': 'Delete', sun: 'Light mode', moon: 'Dark mode' };
$('#icon-grid').innerHTML = Object.entries(ICON_USES).map(([n, use]) =>
  `<div>${icon(n, 'icon--lg')}<span><span class="t-label">${n}</span><br><span class="t-caption muted">${use}</span></span></div>`).join('');

/* ---------- Component specimens (rendered into the dark panel, then cloned to light) ---------- */
const STATES = [['Default', ''], ['Hover', 'is-hover'], ['Focus', 'is-focus'], ['Pressed', 'is-pressed'], ['Disabled', 'is-disabled']];
const BUTTONS = [['primary', 'Primary', 'Write a Review', ''], ['secondary', 'Secondary', 'Submit a Venue', 'plus'], ['ghost', 'Ghost', 'Show all Genres', ''], ['danger', 'Destructive', 'Delete Review', 'trash-2']];
$('#button-matrix').innerHTML = BUTTONS.map(([v, name, text, ic]) =>
  `<div><div class="t-label" style="margin-bottom:var(--space-3)">${name}</div><div class="matrix">${STATES.map(([s, cls]) =>
    `<div><div class="state-label">${s}</div><button type="button" class="btn btn--${v} ${cls}" ${cls === 'is-disabled' ? 'disabled' : ''}>${ic ? icon(ic) : ''}${text}</button></div>`).join('')}</div></div>`).join('');

const labelled = (label, html) => `<div><div class="state-label">${label}</div>${html}</div>`;
$('#card-states').innerHTML = [
  labelled('Default, with photo', venueCard(AMBOSS)),
  labelled('No photo', venueCard(SAITE)),
  labelled('Hover', venueCard({ ...SAITE, extras: null }, 'is-hover')),
  labelled('Focus (keyboard)', venueCard({ ...SAITE, extras: null }, 'is-focus')),
  labelled('Pressed', venueCard({ ...SAITE, extras: null }, 'is-pressed')),
  labelled('Not yet reviewed', venueCard({ ...SAITE, name: 'Kellerloch', type: 'Bar', city: 'Bochum', genres: ['Punk', 'Stoner'], extras: ['pool'], avg: 0, count: 0, distance: '24 km' })),
].join('');

$('#rating-display').innerHTML = [
  labelled('Average', ratingDisplay(4.3, 128)),
  labelled('Single Review', ratingDisplay(5, 1)),
  labelled('Low average', ratingDisplay(1.8, 12)),
  labelled('Not yet reviewed', ratingDisplay(0, 0)),
].join('');
$('#rating-input').innerHTML = [
  labelled('Interactive', rateInput({ live: true })),
  labelled('Default, empty', rateInput()),
  labelled('Hover (previewing 3)', rateInput({ preview: 3 })),
  labelled('Focus (keyboard on 4)', rateInput({ value: 4, focus: 4 })),
  labelled('Pressed', rateInput({ value: 4, pressed: 4 })),
  labelled('Selected', rateInput({ value: 4 })),
  labelled('Disabled', rateInput({ value: 4, disabled: true })),
  labelled('Error', rateInput({ error: 'Choose a rating from 1 to 5.' })),
].join('');

$('#genre-group').innerHTML = genreTags(GENRES);
$('#genre-filter').innerHTML = GENRES.slice(0, 8).map((g, i) => chip(g, { selected: i === 1 || i === 4 })).join('');
$('#genre-filter').dataset.liveChips = '';
$('#chip-states').innerHTML = [
  labelled('Unselected', chip('Doom')),
  labelled('Hover', chip('Doom', { state: 'is-hover' })),
  labelled('Focus', chip('Doom', { state: 'is-focus' })),
  labelled('Pressed', chip('Doom', { state: 'is-pressed' })),
  labelled('Selected', chip('Doom', { selected: true })),
  labelled('Selected, hover', chip('Doom', { selected: true, state: 'is-hover' })),
  labelled('Disabled', chip('Doom', { disabled: true })),
].join('');

$('#badge-slot').innerHTML = verifiedBadge();
document.querySelectorAll('.badge-slot').forEach(el => { el.innerHTML = verifiedBadge(); });

const checkin = (cls = '', attrs = '') => `<button type="button" class="btn btn--primary btn--lg ${cls}" ${attrs}>${icon('map-pin-check')}Check in</button>`;
$('#checkin-states').innerHTML = [
  labelled('Default', checkin()),
  labelled('Hover', checkin('is-hover')),
  labelled('Focus', checkin('is-focus')),
  labelled('Pressed', checkin('is-pressed')),
  labelled('Disabled', checkin('', 'disabled')),
  labelled('Done', `<button type="button" class="btn btn--lg btn--done" aria-disabled="true">${checkinDone('21:14')}</button>`),
].join('');

$('#identity-states').innerHTML = [
  labelled('Compact, initials', identity(KAI)),
  labelled('Compact, photo slot', identity(MIRA, 'compact', true)),
  labelled('Compact, long display name', `<div style="max-width:280px">${identity(LONG)}</div>`),
  labelled('Full, initials', identity(KAI, 'full')),
  labelled('Full, photo slot', identity(MIRA, 'full', true)),
  labelled('Full, long display name', `<div style="max-width:280px">${identity(LONG, 'full')}</div>`),
].join('');

$('#review-list').innerHTML = [
  { user: KAI, rating: 5, date: '12 Oct 2026', iso: '2026-10-12', text: 'Cold Altbier, the dartboard is in good shape, and they played Reign in Blood front to back. Gets loud after ten, in a good way.' },
  { user: MIRA, rating: 4, date: '3 Oct 2026', iso: '2026-10-03', text: 'Doom on Thursdays is the reason to come. The kitchen closes at 22:00, so eat early.' },
  { user: { name: 'Jonas Becker', username: 'jonas_b' }, rating: 2, date: '28 Sep 2026', iso: '2026-09-28', removed: true },
  { user: { name: 'Lena Vogt', username: 'lena_thrash' }, rating: 3, date: '20 Sep 2026', iso: '2026-09-20' },
].map(review).join('') + '<p class="t-caption muted" style="margin-top:var(--space-3)">Third: commentary removed by a moderator. Fourth: rating only, since commentary is optional.</p>';

const skeletonCard = () => `<div class="skeleton-card" aria-hidden="true">
    <div class="skeleton" style="width:110px;height:22px"></div>
    <div class="skeleton" style="width:75%;height:30px"></div>
    <div class="skeleton" style="width:55%;height:16px"></div>
    <div style="display:flex;gap:8px"><div class="skeleton" style="width:90px;height:32px"></div><div class="skeleton" style="width:70px;height:32px"></div></div>
  </div>`;
$('#skeletons').innerHTML = skeletonCard() + skeletonCard();

/* ---------- Dark + light panels and the theme toggle ---------- */
for (const panels of document.querySelectorAll('.panels')) {
  const light = panels.querySelector('[data-theme="dark"]').cloneNode(true);
  light.dataset.theme = 'light';
  light.querySelectorAll('[id]').forEach(el => el.removeAttribute('id'));
  panels.append(light);
}
function setView(view) {
  document.documentElement.dataset.view = view;
  document.documentElement.dataset.theme = view === 'light' ? 'light' : 'dark';
  document.querySelectorAll('.seg button').forEach(b => b.setAttribute('aria-pressed', b.dataset.view === view));
  try { localStorage.setItem('mh-view', view); } catch {}
}
$('.seg').addEventListener('click', (e) => { const b = e.target.closest('button'); if (b) setView(b.dataset.view); });
let saved = null;
try { saved = localStorage.getItem('mh-view'); } catch {}
setView(new URLSearchParams(location.search).get('view') || saved || 'both');

/* ---------- Live behaviour (delegated, so the cloned light panels work too) ---------- */
function setRating(group, n) {
  group.querySelectorAll('.rate__star').forEach((s, i) => {
    s.classList.toggle('is-on', i < n);
    s.setAttribute('aria-checked', i + 1 === n);
    s.tabIndex = i + 1 === n ? 0 : -1;
  });
  group.closest('.rate-field').querySelector('.rate__value').textContent = `${n} of 5`;
}
document.addEventListener('click', (e) => {
  const star = e.target.closest('.rate[data-live] .rate__star');
  if (star) setRating(star.parentElement, [...star.parentElement.children].indexOf(star) + 1);
  const c = e.target.closest('[data-live-chips] .chip');
  if (c) c.setAttribute('aria-pressed', c.getAttribute('aria-pressed') !== 'true');
  const ci = e.target.closest('.checkin-live:not(.btn--done)');
  if (ci) {
    ci.className = 'btn btn--lg btn--done checkin-live checkin-pop';
    ci.setAttribute('aria-disabled', 'true');
    ci.innerHTML = checkinDone(new Date().toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' }));
  }
});
document.addEventListener('keydown', (e) => {
  const star = e.target.closest('.rate[data-live] .rate__star');
  if (!star) return;
  const group = star.parentElement, i = [...group.children].indexOf(star);
  const next = { ArrowRight: i + 1, ArrowUp: i + 1, ArrowLeft: i - 1, ArrowDown: i - 1, Home: 0, End: 4 }[e.key];
  if (next === undefined) return;
  e.preventDefault();
  const n = Math.max(0, Math.min(4, next));
  setRating(group, n + 1);
  group.children[n].focus();
});
document.addEventListener('mouseover', (e) => {
  const star = e.target.closest('.rate[data-live] .rate__star');
  const group = e.target.closest('.rate[data-live]');
  if (!group) return;
  const n = star ? [...group.children].indexOf(star) + 1 : 0;
  group.querySelectorAll('.rate__star').forEach((s, i) => s.classList.toggle('is-preview', i < n));
});
document.addEventListener('mouseout', (e) => {
  const group = e.target.closest('.rate[data-live]');
  if (group && !group.contains(e.relatedTarget)) group.querySelectorAll('.is-preview').forEach(s => s.classList.remove('is-preview'));
});
