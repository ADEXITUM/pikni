// Собирает demo.html — страницу прослушивания звуков, которая открывается
// двойным щелчком (без сервера). Код звуков встраивается из pikni-sounds.js,
// поэтому после правки звуков страницу пересобирают: node build-demo.mjs
import { readFileSync, writeFileSync } from 'node:fs'

const here = new URL('.', import.meta.url)
const engine = readFileSync(new URL('pikni-sounds.js', here), 'utf8').replace(/^export /gm, '')

const html = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Звуки Пикни</title>
<style>
  :root { --bg:#F3EDE3; --card:#FFFDF8; --ink:#221D17; --muted:#5B5246; --primary:#2D3A8C; --line:#D9CDB8; }
  @media (prefers-color-scheme: dark) { :root { --bg:#14110D; --card:#1D1914; --ink:#F1E9DC; --muted:#CDC3B2; --primary:#AEB8FF; --line:#3A332A; } }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px 16px; background:var(--bg); color:var(--ink); font:16px/1.5 Manrope, system-ui, sans-serif; }
  main { max-width:720px; margin:0 auto; }
  h1 { font-size:1.75rem; margin:0 0 4px; }
  p { color:var(--muted); margin:0 0 20px; }
  .row { display:flex; gap:12px; align-items:center; padding:12px 16px; background:var(--card); border:1px solid var(--line); border-radius:8px; margin-bottom:8px; }
  .row b { flex:1; }
  .row small { display:block; color:var(--muted); font-weight:400; }
  button { min-height:44px; padding:0 16px; border:0; border-radius:8px; background:var(--primary); color:var(--card); font:700 1rem Manrope, system-ui, sans-serif; cursor:pointer; }
  a { color:var(--primary); font-weight:700; }
  label { display:flex; gap:12px; align-items:center; margin:16px 0; }
  code { font-family:'JetBrains Mono', ui-monospace, monospace; }
</style>
</head>
<body>
<main>
  <h1>Звуки сканера «Пикни»</h1>
  <p>«Ток» — всё хорошо, «пум» — что-то не так. Звуки синтезируются в браузере, файлов нет. Подробности — в sounds.md.</p>
  <label>Громкость <input id="vol" type="range" min="0" max="1" step="0.05" value="0.8"></label>
  <label><input id="hap" type="checkbox" checked> Вибрация (Android)</label>
  <div id="list"></div>
  <p><button id="all">Сыграть все подряд</button></p>
</main>
<script>
${engine}
const sounds = createPikniSounds()
const list = document.getElementById('list')
for (const [key, e] of Object.entries(EVENTS)) {
  const row = document.createElement('div')
  row.className = 'row'
  row.innerHTML = '<b>' + e.label + '<small><code>' + key + '</code> · вибрация ' + e.vibrate.join('/') + ' мс</small></b>'
  const play = document.createElement('button')
  play.textContent = 'Слушать'
  play.onclick = async () => { await sounds.unlock(); sounds.play(key) }
  const wav = document.createElement('a')
  wav.textContent = 'WAV'
  wav.href = '#'
  wav.onclick = async ev => {
    ev.preventDefault()
    const blob = await renderToWav(key)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'pikni-' + key + '.wav'
    a.click()
  }
  row.append(play, wav)
  list.append(row)
}
document.getElementById('vol').oninput = ev => sounds.setVolume(Number(ev.target.value))
document.getElementById('hap').onchange = ev => sounds.setHaptics(ev.target.checked)
document.getElementById('all').onclick = async () => {
  await sounds.unlock()
  let delay = 0
  for (const key of Object.keys(EVENTS)) { setTimeout(() => sounds.play(key), delay); delay += 1100 }
}
</script>
</body>
</html>
`
writeFileSync(new URL('demo.html', here), html, 'utf8')
console.log('demo.html собран')
