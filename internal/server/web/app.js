// PiCameras: interfaz web. Sin dependencias ni paso de compilación.
// Regla: los datos (nombres de cámaras, respuestas del servidor) se pintan siempre con textContent, nunca con
// innerHTML.
import {VideoRTC} from './vendor/video-rtc.js';

// ---- reproductor ----

class CamVideo extends VideoRTC {
    constructor() {
        super();
        // Solo MSE/MP4 por el WebSocket autenticado: sin WebRTC (no abrimos puertos UDP) ni HLS.
        this.mode = 'mse,mp4';
        this.media = 'video';
        this.pcConfig = {iceServers: []};
        this.showControls = false;
    }

    oninit() {
        super.oninit();
        this.video.muted = true;
        this.video.playsInline = true;
        this.video.controls = this.showControls;
        this.statusEl = el('div', {class: 'cam-status'}, 'Conectando…');
        this.appendChild(this.statusEl);
        this.video.addEventListener('playing', () => { this.statusEl.hidden = true; });
        this.video.addEventListener('waiting', () => { this.statusEl.hidden = false; this.statusEl.textContent = 'Cargando…'; });
    }

    onclose() {
        if (this.statusEl) {
            this.statusEl.hidden = false;
            this.statusEl.textContent = 'Sin señal. Reintentando…';
        }
        return super.onclose();
    }
}
customElements.define('cam-video', CamVideo);

// ---- utilidades ----

function el(tag, attrs = {}, ...children) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (v === false || v == null) continue;
        if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
        else e.setAttribute(k, v === true ? '' : v);
    }
    for (const c of children.flat()) {
        if (c == null || c === false) continue;
        e.append(c instanceof Node ? c : document.createTextNode(String(c)));
    }
    return e;
}

const $ = (sel, root = document) => root.querySelector(sel);
const view = $('#view');

async function api(method, path, body) {
    const opts = {method, headers: {'X-PiCameras': '1'}, credentials: 'same-origin'};
    if (body !== undefined) {
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    let data = null;
    try { data = await res.json(); } catch { /* sin cuerpo */ }
    if (res.status === 401 && path !== '/api/login' && path !== '/api/setup' && path !== '/api/password') {
        route();
        throw new Error(data?.error || 'inicia sesión');
    }
    if (!res.ok) throw new Error(data?.error || `error ${res.status}`);
    return data;
}

function busy(btn, on, label) {
    if (!btn) return;
    if (on) { btn.dataset.label = btn.textContent; btn.textContent = label || 'Un momento…'; btn.disabled = true; }
    else { btn.textContent = btn.dataset.label || btn.textContent; btn.disabled = false; }
}

// ---- enrutado sencillo ----

let state = {user: '', cameras: [], meta: null};

async function route() {
    stopViewer();
    let s;
    try {
        s = await (await fetch('/api/state', {credentials: 'same-origin'})).json();
    } catch {
        view.replaceChildren(el('p', {class: 'empty'}, 'No puedo hablar con PiCameras. ¿Está en marcha?'));
        return;
    }
    $('.topbar').hidden = !s.user;
    if (s.needsSetup) return renderSetup();
    if (!s.user) return renderLogin();
    state.user = s.user;
    if (!state.meta) state.meta = await api('GET', '/api/meta');
    await loadCameras();
}

// ---- configuración inicial ----

function authCard(title, intro, fields, submitLabel, onSubmit) {
    const err = el('p', {class: 'error', role: 'alert'});
    const btn = el('button', {type: 'submit', class: 'btn primary'}, submitLabel);
    const form = el('form', {class: 'auth-card', novalidate: true},
        el('h1', {}, el('img', {src: 'icon.svg', alt: '', width: 28, height: 28}), title),
        intro, ...fields, err, btn);
    form.addEventListener('submit', async (ev) => {
        ev.preventDefault();
        err.textContent = '';
        busy(btn, true);
        try { await onSubmit(new FormData(form)); }
        catch (e) { err.textContent = e.message; }
        finally { busy(btn, false); }
    });
    view.replaceChildren(el('div', {class: 'auth'}, form));
    form.querySelector('input')?.focus();
}

function field(label, name, type = 'text', extra = {}) {
    return el('label', {}, label, el('input', {name, type, required: true, ...extra}));
}

function renderSetup() {
    authCard('Bienvenido a PiCameras',
        el('p', {}, 'Crea tu usuario. Necesitas el código de configuración que PiCameras muestra al arrancar ',
            '(en la consola, o en ', el('code', {}, 'setup-code.txt'), ' dentro de su carpeta de datos).'),
        [
            field('Código de configuración', 'code', 'text', {autocomplete: 'off', spellcheck: 'false', placeholder: 'XXXX-XXXX-XXXX-XXXX'}),
            field('Usuario', 'username', 'text', {autocomplete: 'username', maxlength: 32}),
            field('Contraseña (mínimo 10 caracteres)', 'password', 'password', {autocomplete: 'new-password', minlength: 10}),
            field('Repite la contraseña', 'password2', 'password', {autocomplete: 'new-password'}),
        ],
        'Crear usuario',
        async (fd) => {
            if (fd.get('password') !== fd.get('password2')) throw new Error('Las contraseñas no coinciden');
            await api('POST', '/api/setup', {code: fd.get('code'), username: fd.get('username'), password: fd.get('password')});
            route();
        });
}

function renderLogin() {
    authCard('PiCameras', null,
        [
            field('Usuario', 'username', 'text', {autocomplete: 'username'}),
            field('Contraseña', 'password', 'password', {autocomplete: 'current-password'}),
        ],
        'Entrar',
        async (fd) => {
            await api('POST', '/api/login', {username: fd.get('username'), password: fd.get('password')});
            route();
        });
}

// ---- mosaico ----

async function loadCameras() {
    state.cameras = await api('GET', '/api/cameras');
    renderHome();
}

function liveURL(cam, quality) {
    return `/api/cameras/${encodeURIComponent(cam.id)}/live` + (quality === 'sub' ? '?q=sub' : '');
}

function renderHome() {
    if (!state.cameras.length) {
        view.replaceChildren(el('div', {class: 'empty'},
            el('h2', {}, 'Todavía no hay cámaras'),
            el('p', {}, 'Busco las de tu red y te digo cómo darlas de alta. También puedes añadirlas a mano.'),
            el('button', {class: 'btn primary', onclick: openDiscover}, 'Buscar cámaras')));
        return;
    }
    const bySite = new Map();
    for (const c of state.cameras) {
        if (!bySite.has(c.site)) bySite.set(c.site, []);
        bySite.get(c.site).push(c);
    }
    const sections = [...bySite.entries()]
        .sort(([a], [b]) => a.localeCompare(b, 'es'))
        .map(([site, cams]) => el('section', {class: 'site'},
            bySite.size > 1 ? el('h2', {}, site) : null,
            el('div', {class: 'tiles'}, cams.map(tile))));
    view.replaceChildren(el('div', {class: 'home'}, sections));
}

function tile(cam) {
    const player = document.createElement('cam-video');
    player.src = liveURL(cam, cam.hasSub ? 'sub' : 'main');
    return el('button', {type: 'button', class: 'tile', 'aria-label': `Ver ${cam.name} en grande`, onclick: () => openViewer(cam)},
        player, el('span', {class: 'tile-label'}, cam.name));
}

// ---- vista de una cámara ----

let viewerEl = null;

function openViewer(cam) {
    stopViewer();
    const player = document.createElement('cam-video');
    player.media = 'video,audio';
    player.showControls = true;
    player.src = liveURL(cam, 'main');
    viewerEl = el('div', {class: 'viewer', role: 'dialog', 'aria-label': cam.name},
        el('div', {class: 'viewer-bar'},
            el('span', {class: 'title'}, `${cam.name} · ${cam.site}`),
            el('button', {class: 'btn', onclick: () => { const c = cam; stopViewer(); openCameraForm(c); }}, 'Editar'),
            el('button', {class: 'btn', onclick: () => player.video?.requestFullscreen?.()}, 'Pantalla completa'),
            el('button', {class: 'btn', onclick: stopViewer, 'aria-label': 'Cerrar'}, 'Cerrar')),
        el('div', {class: 'viewer-stage'}, player));
    document.body.append(viewerEl);
    document.addEventListener('keydown', escCloses);
}

function escCloses(ev) { if (ev.key === 'Escape') stopViewer(); }

function stopViewer() {
    if (!viewerEl) return;
    viewerEl.remove();
    viewerEl = null;
    document.removeEventListener('keydown', escCloses);
}

// ---- buscar cámaras ----

const discoverDialog = $('#discover-dialog');

function openDiscover() {
    $('[data-role=found]', discoverDialog).replaceChildren();
    $('[data-role=discover-status]', discoverDialog).textContent = '';
    discoverDialog.showModal();
}

async function scan(prefix, btn) {
    const status = $('[data-role=discover-status]', discoverDialog);
    const list = $('[data-role=found]', discoverDialog);
    list.replaceChildren();
    status.textContent = prefix ? `Buscando en ${prefix}… (hasta medio minuto)` : 'Buscando en tu red… (unos segundos)';
    busy(btn, true, 'Buscando…');
    try {
        const res = await api('POST', '/api/discover', {prefix: prefix || ''});
        const found = res.found || [];
        status.textContent = found.length
            ? `He encontrado ${found.length} ${found.length === 1 ? 'posible cámara' : 'posibles cámaras'}.`
            : 'No he encontrado cámaras. Si es una cámara Wi-Fi de marca (YI, Xiaomi, Tuya…), de serie no abre ' +
              'RTSP: mira la guía de tu modelo. Si no, añádela a mano.';
        for (const f of found) list.append(foundItem(f, res.known?.[f.ip]));
    } catch (e) {
        status.textContent = e.message;
    } finally {
        busy(btn, false);
    }
}

const kindNames = {rtsp: 'RTSP', onvif: 'ONVIF', yihack: 'YI (yi-hack)', http: 'HTTP'};

function foundItem(f, known) {
    const title = f.name || f.vendor || f.hardware || 'Cámara';
    const details = [f.ip, f.hardware && f.hardware !== title ? f.hardware : null,
        f.onvif ? `ONVIF (puerto ${f.suggest.port || 80})` : null,
        f.ports?.length ? `puertos ${f.ports.join(', ')}` : null].filter(Boolean).join(' · ');
    return el('li', {},
        el('div', {class: 'what'}, el('strong', {}, title), el('span', {}, details)),
        known ? el('span', {class: 'badge'}, 'Ya añadida')
            : el('button', {type: 'button', class: 'btn primary', onclick: () => {
                discoverDialog.close();
                openCameraForm(null, {
                    name: f.name || '', host: f.ip, kind: f.suggest.kind,
                    port: f.suggest.port && ![554, 80].includes(f.suggest.port) ? f.suggest.port : '',
                });
            }}, `Añadir (${kindNames[f.suggest.kind] || f.suggest.kind})`));
}

discoverDialog.addEventListener('click', (ev) => {
    const a = ev.target.closest('[data-action]')?.dataset.action;
    if (a === 'scan-local') scan('', ev.target);
    if (a === 'scan-remote') {
        const p = discoverDialog.querySelector('[name=prefix]').value.trim();
        if (!p) { $('[data-role=discover-status]', discoverDialog).textContent = 'Escribe la red, por ejemplo 192.168.99.0/24'; return; }
        scan(p, ev.target);
    }
    if (a === 'manual') { discoverDialog.close(); openCameraForm(null, {}); }
});

// ---- alta y edición ----

const camDialog = $('#camera-dialog');
const camForm = $('form', camDialog);
let editing = null;

const defaultPorts = {rtsp: '554', onvif: '80', yihack: '554', http: '80'};

function syncKind() {
    const kind = camForm.elements.kind.value;
    for (const n of camForm.querySelectorAll('[data-kinds]')) {
        n.hidden = !n.dataset.kinds.split(' ').includes(kind);
    }
    camForm.elements.port.placeholder = defaultPorts[kind];
    camForm.elements.path.placeholder = kind === 'http' ? '/video.mjpg' : '/stream1';
}

function fillPresets() {
    const sel = camForm.elements.preset;
    if (sel.options.length > 1 || !state.meta) return;
    for (const p of state.meta.presets) sel.append(el('option', {value: p.brand}, p.brand));
}

function openCameraForm(cam, prefill = {}) {
    editing = cam;
    fillPresets();
    camForm.reset();
    const v = cam || prefill;
    for (const k of ['name', 'site', 'host', 'port', 'path', 'subPath', 'username']) {
        if (camForm.elements[k]) camForm.elements[k].value = v[k] ?? '';
    }
    camForm.elements.kind.value = v.kind || 'rtsp';
    camForm.elements.password.placeholder = cam?.hasPassword ? 'Sin cambios' : '';
    const sites = new Set(['Casa', ...state.cameras.map((c) => c.site)]);
    $('#sites').replaceChildren(...[...sites].map((s) => el('option', {value: s})));
    $('#camera-title').textContent = cam ? `Editar ${cam.name}` : 'Nueva cámara';
    $('[data-action=delete]', camDialog).hidden = !cam;
    $('[data-role=camera-error]', camDialog).textContent = '';
    $('[data-role=test-result]', camDialog).hidden = true;
    syncKind();
    camDialog.showModal();
    (camForm.elements.name.value ? camForm.elements.host : camForm.elements.name).focus();
}

function formInput() {
    const f = camForm;
    return {
        name: f.name.value, site: f.site.value, kind: f.kind.value, host: f.host.value,
        port: parseInt(f.port.value, 10) || 0, path: f.path.value, subPath: f.subPath.value,
        username: f.username.value, password: f.password.value, keepPassword: !!editing && f.password.value === '',
    };
}

camForm.elements.kind.addEventListener('change', syncKind);
camForm.elements.preset.addEventListener('change', () => {
    const p = state.meta?.presets.find((x) => x.brand === camForm.elements.preset.value);
    if (!p) return;
    camForm.elements.path.value = p.path;
    camForm.elements.subPath.value = p.subPath;
});

camDialog.addEventListener('click', async (ev) => {
    const a = ev.target.closest('[data-action]')?.dataset.action;
    const err = $('[data-role=camera-error]', camDialog);
    if (a === 'close') camDialog.close();
    if (a === 'test') {
        err.textContent = '';
        const box = $('[data-role=test-result]', camDialog);
        busy(ev.target, true, 'Probando…');
        try {
            const r = await api('POST', '/api/cameras/test', {...formInput(), id: editing?.id || ''});
            box.hidden = false;
            box.className = 'test-result ' + (r.ok ? 'ok' : 'bad');
            $('p', box).textContent = r.ok ? '¡Conectada! Este es el último fotograma.' : r.error;
            const video = $('video', box);
            if (video.src) URL.revokeObjectURL(video.src);
            if (r.ok) {
                const bytes = Uint8Array.from(atob(r.frame), (c) => c.charCodeAt(0));
                video.src = URL.createObjectURL(new Blob([bytes], {type: r.contentType || 'video/mp4'}));
            }
        } catch (e) {
            err.textContent = e.message;
        } finally {
            busy(ev.target, false);
        }
    }
    if (a === 'delete' && editing) {
        if (!confirm(`¿Borrar ${editing.name}?`)) return;
        try {
            await api('DELETE', `/api/cameras/${encodeURIComponent(editing.id)}`);
            camDialog.close();
            await loadCameras();
        } catch (e) { err.textContent = e.message; }
    }
});

camForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const err = $('[data-role=camera-error]', camDialog);
    err.textContent = '';
    const btn = ev.submitter;
    busy(btn, true, 'Guardando…');
    try {
        if (editing) await api('PUT', `/api/cameras/${encodeURIComponent(editing.id)}`, formInput());
        else await api('POST', '/api/cameras', formInput());
        camDialog.close();
        await loadCameras();
    } catch (e) {
        err.textContent = e.message;
    } finally {
        busy(btn, false);
    }
});

// ---- cuenta ----

const accountDialog = $('#account-dialog');
const accountForm = $('form', accountDialog);
accountDialog.addEventListener('click', (ev) => {
    if (ev.target.closest('[data-action=close]')) accountDialog.close();
});
accountForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const err = $('.error', accountDialog);
    err.textContent = '';
    try {
        await api('POST', '/api/password', {current: accountForm.elements.current.value, new: accountForm.elements.new.value});
        accountDialog.close();
    } catch (e) { err.textContent = e.message; }
});

// ---- barra superior ----

$('.topbar').addEventListener('click', async (ev) => {
    const a = ev.target.closest('[data-action]')?.dataset.action;
    if (a === 'add') openDiscover();
    if (a === 'account') { accountForm.reset(); $('.error', accountDialog).textContent = ''; accountDialog.showModal(); }
    if (a === 'logout') {
        try { await api('POST', '/api/logout'); } finally { route(); }
    }
});

route();
