import type { CallState } from '@/call/session'

// Dev-only fixture for call.html?demo=transcript: a call in progress with a
// finished exchange and a question being recognised. Synthetic text, loaded
// via a dynamic import behind import.meta.env.DEV — absent from the build.
export function callDemoState(): Partial<CallState> {
  const started = Date.now() - 74_000
  return {
    connection: 'open',
    phase: 'listening',
    input: 'voice',
    site: 'GTP',
    mic: 'live',
    micLevel: 0.55,
    userSpeaking: true,
    startedAt: started,
    messages: [
      { id: 1, role: 'user', text: 'Когда выплачивают аванс?' },
      { id: 2, role: 'bot', text: 'Секунду, смотрю в регламентах.', filler: true, turn: 1 },
      {
        id: 3,
        role: 'bot',
        turn: 1,
        text: 'Аванс выплачивается двадцать пятого числа каждого месяца, зарплата — десятого числа следующего месяца. Это пункт 6.7.4 правил внутреннего трудового распорядка.',
        sources: [
          { doc: 'pvtr.pdf', clause_id: '6.7.4', page: 18, label: 'ПВТР 6.7.4' },
          {
            doc: 'polozhenie-oplata.pdf',
            clause_id: '3.2',
            page: 4,
            label: 'Положение об оплате 3.2',
          },
        ],
      },
      { id: 4, role: 'user', text: 'Можно разделить отпуск на части?' },
      {
        id: 5,
        role: 'bot',
        turn: 2,
        text: 'Да, отпуск можно разделить на части, но одна из них должна быть не меньше четырнадцати календарных дней. Это пункт 7.3 правил внутреннего трудового распорядка.',
        sources: [{ doc: 'pvtr.pdf', clause_id: '7.3', page: 21, label: 'ПВТР 7.3' }],
      },
      { id: 6, role: 'user', text: 'а если я заболел во время', partial: true },
    ],
  }
}
