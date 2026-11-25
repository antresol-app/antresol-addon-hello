// The hello addon's web part: a page that greets the profile through the
// addon's own API (/api/hello) and sends «ping», which the addon publishes
// through the core API and the page sees come back as addon.hello.ping; a
// tile in «Управление»; and an item action that answers with the title.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { addonApi, registerAddon, ui, useAddonT, useEvents } from '@antresol/addon-sdk'
import styles from './index.module.css'

const ID = 'hello'
const api = addonApi(ID)

const strings = {
  ru: {
    title: 'Привет',
    hello: 'Привет, {name}',
    ping: 'Отправить ping',
    pinged: 'Пришло событие ping от {name}',
    failed: 'Не удалось связаться с расширением',
    action: 'Привет от расширения',
    actionSaid: 'Привет от расширения: {title}',
    sample: 'Пример видео',
    sampleHls: 'Пример видео (HEVC)',
    note: 'Пример от расширения «Привет»',
  },
  en: {
    title: 'Hello',
    hello: 'Hello, {name}',
    ping: 'Send ping',
    pinged: 'A ping event came from {name}',
    failed: 'Could not reach the addon',
    action: 'Hello from the addon',
    actionSaid: 'Hello from the addon: {title}',
    sample: 'Sample video',
    sampleHls: 'Sample video (HEVC)',
    note: 'A sample from the «Hello» addon',
  },
}
// labels and titles are fixed when the bundle registers: the app's language then
const s = strings[document.documentElement.lang === 'en' ? 'en' : 'ru']

function Page() {
  const t = useAddonT(ID)
  const [name, setName] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  const [sending, setSending] = useState(false)
  const navigate = useNavigate()
  const [from, setFrom] = useState<string | null>(null)
  useEffect(() => {
    api.get<{ name: string }>('hello').then((r) => setName(r.name), () => setFailed(true))
  }, [])
  useEvents((e) => setFrom((e.data as { from?: string }).from ?? ''), ['addon.hello.ping'])
  const ping = async () => {
    setSending(true)
    setFailed(false)
    try {
      await api.post('ping')
    } catch {
      setFailed(true)
    } finally {
      setSending(false)
    }
  }
  // registers a sample input for this profile and opens the core player on it
  const play = async (which: 'direct' | 'hls') => {
    setFailed(false)
    try {
      const { id } = await api.post<{ id: string }>('sample/' + which)
      navigate('/play/input/' + id)
    } catch {
      setFailed(true)
    }
  }
  return (
    <section className={styles.page}>
      {name !== null && <h1 className={styles.title}>{t('hello', { name })}</h1>}
      <ui.Button icon="send" loading={sending} onClick={() => void ping()}>
        {t('ping')}
      </ui.Button>
      <ui.Button icon="play" onClick={() => void play('direct')}>
        {t('sample')}
      </ui.Button>
      <ui.Button icon="play" onClick={() => void play('hls')}>
        {t('sampleHls')}
      </ui.Button>
      {from !== null && <ui.Notice tone="accent">{t('pinged', { name: from })}</ui.Notice>}
      {failed && <ui.Notice tone="destructive">{t('failed')}</ui.Notice>}
    </section>
  )
}

registerAddon(ID, {
  routes: [{ path: 'page', title: s.title, element: Page }],
  manageTiles: [{ label: s.title, icon: 'puzzle', tint: 'gray', to: 'page' }],
  // the text run() returns shows in a notice under the item's actions
  itemActions: [{ label: s.action, icon: 'sparkles', run: (item) => s.actionSaid.replace('{title}', item.title) }],
  // shown while one of this addon's inputs loads in the core player
  playerNotes: [{ element: () => <ui.Notice tone="accent">{s.note}</ui.Notice> }],
  events: ['ping'],
  strings,
})
