// Original inline SVG cartoons. Every colour comes from CSS variables (styles/v2.css, ".cn" rules), so they follow
// the platform brand (primary, accent, success, info) and light / dark mode. Simple shapes only – no external assets.
import { ReactNode } from 'react'

type P = { w?: number | string; className?: string; title?: string }

function Svg({ w = 200, className = '', title, vb = '0 0 200 160', children }: P & { vb?: string; children: ReactNode }) {
  return <svg className={'cn ' + className} viewBox={vb} width={w} style={{ height: 'auto', maxWidth: '100%' }} role={title ? 'img' : undefined} aria-hidden={title ? undefined : true} aria-label={title}>
    {title && <title>{title}</title>}
    {children}
  </svg>
}

/** Ground shadow under a character. */
const Shadow = ({ x = 100, y = 150, rx = 60 }: { x?: number; y?: number; rx?: number }) => <ellipse cx={x} cy={y} rx={rx} ry={6} className="cn-sh" />

/** A busy salesperson riding a rocket, tie flapping, phone in hand. */
export function RocketRider(p: P) {
  return <Svg {...p}>
    <Shadow y={152} rx={54} />
    <g className="cn-spd">
      <path className="cn-ln" d="M8 70h26M2 88h30M14 106h22" />
    </g>
    <g className="cn-bob">
      <g transform="rotate(-14 110 100)">
        {/* rider */}
        <path className="cn-p cn-o" d="M104 56 L80 47 L86 60 L76 66 L104 62 Z" />
        <rect className="cn-lt cn-o" x="92" y="50" width="26" height="32" rx="9" />
        <path className="cn-p cn-o" d="M105 54 l-4 12 4 6 4-6z" />
        <path className="cn-o" fill="none" d="M116 60 L130 40" />
        <rect className="cn-dk cn-o" x="125" y="24" width="10" height="17" rx="3" />
        <rect className="cn-a" x="127.5" y="27" width="5" height="7" rx="1" />
        <path className="cn-o" fill="none" d="M95 62 L84 74" />
        <circle className="cn-k cn-o" cx="105" cy="38" r="13" />
        <path className="cn-hr cn-o" d="M92 36 Q94 21 107 23 Q119 24 118 34 Q110 29 100 31 Q95 32 92 36Z" />
        <circle className="cn-dkf" cx="101" cy="38" r="1.8" /><circle className="cn-dkf" cx="110" cy="38" r="1.8" />
        <path className="cn-dkf" d="M99 43 Q105 51 112 43Z" />
        {/* rocket */}
        <path className="cn-a cn-o cn-flame" d="M44 92 Q4 100 44 112 Q26 102 44 92Z" />
        <path className="cn-wn cn-flame2" d="M44 97 Q24 102 44 107Z" />
        <path className="cn-pd cn-o" d="M62 80 L44 58 L84 80Z" />
        <path className="cn-pd cn-o" d="M62 120 L44 142 L84 120Z" />
        <path className="cn-p cn-o" d="M44 100 Q44 80 72 79 L142 79 Q176 82 188 100 Q176 118 142 121 L72 121 Q44 120 44 100Z" />
        <path className="cn-hl" d="M76 87 L140 87 Q160 88 170 95" />
        <circle className="cn-lt cn-o" cx="150" cy="100" r="10" />
        <circle className="cn-i" cx="150" cy="100" r="5.5" />
        <path className="cn-lt cn-o" d="M104 82 Q100 96 112 98" fill="none" />
      </g>
    </g>
    <g className="cn-flt"><rect className="cn-lt cn-o" x="160" y="22" width="22" height="16" rx="3" transform="rotate(12 171 30)" /><path className="cn-ok-s" d="M165 31l4 3 7-7" transform="rotate(12 171 30)" /></g>
    <g className="cn-flt d2"><circle className="cn-a cn-o" cx="30" cy="34" r="9" /><path className="cn-dks" d="M30 29v10M27 31h5a2 2 0 0 1 0 4h-4a2 2 0 0 0 0 4h5" /></g>
  </Svg>
}

/** A coffee-drinking robot sorting lead cards into trays. */
export function CoffeeBot(p: P) {
  return <Svg {...p}>
    <Shadow y={150} rx={64} />
    {/* trays */}
    <g>
      <rect className="cn-g cn-o" x="132" y="118" width="46" height="18" rx="5" />
      <rect className="cn-lt cn-o" x="138" y="108" width="34" height="14" rx="3" />
      <path className="cn-ok-s" d="M146 115l4 3 7-6" />
    </g>
    <g className="cn-bob">
      {/* antenna */}
      <path className="cn-o" fill="none" d="M96 26 V14" />
      <circle className="cn-p cn-o cn-blink" cx="96" cy="11" r="5" />
      {/* head */}
      <rect className="cn-lt cn-o" x="70" y="26" width="52" height="38" rx="13" />
      <rect className="cn-dk" x="77" y="33" width="38" height="22" rx="9" />
      <circle className="cn-a" cx="88" cy="44" r="4.5" />
      <path className="cn-a-s" d="M100 45 q4-5 8 0" />
      {/* body */}
      <rect className="cn-md cn-o" x="72" y="66" width="48" height="46" rx="12" />
      <rect className="cn-p cn-o" x="84" y="76" width="24" height="14" rx="4" />
      <circle className="cn-a" cx="90" cy="83" r="2.2" /><circle className="cn-g" cx="96" cy="83" r="2.2" /><circle className="cn-lt" cx="102" cy="83" r="2.2" />
      <path className="cn-o" fill="none" d="M84 98h24" />
      {/* arm with mug */}
      <path className="cn-o" fill="none" d="M72 78 Q56 82 54 96" />
      <rect className="cn-p cn-o" x="40" y="94" width="20" height="20" rx="4" />
      <path className="cn-o" fill="none" d="M40 99 q-7 1 -6 7 q1 5 6 4" />
      <path className="cn-stm" d="M46 88 q-4-5 0-10 q4-5 0-10" />
      <path className="cn-stm d2" d="M54 88 q-4-5 0-10 q4-5 0-10" />
      {/* arm with lead card */}
      <path className="cn-o" fill="none" d="M120 78 Q136 74 142 86" />
      <g className="cn-card">
        <rect className="cn-lt cn-o" x="134" y="82" width="30" height="20" rx="3" transform="rotate(-10 149 92)" />
        <circle className="cn-i" cx="142" cy="90" r="3.5" transform="rotate(-10 149 92)" />
        <path className="cn-md-s" d="M148 88h10M148 94h7" transform="rotate(-10 149 92)" />
      </g>
      {/* wheels */}
      <rect className="cn-dk cn-o" x="76" y="112" width="40" height="12" rx="6" />
      <circle className="cn-md cn-o" cx="84" cy="130" r="8" /><circle className="cn-md cn-o" cx="108" cy="130" r="8" />
    </g>
    <rect className="cn-a cn-o" x="16" y="120" width="34" height="16" rx="5" />
    <rect className="cn-lt cn-o" x="20" y="110" width="26" height="12" rx="3" transform="rotate(-6 33 116)" />
  </Svg>
}

/** A calendar juggling meetings, sweating a little. */
export function CalendarJuggler(p: P) {
  return <Svg {...p}>
    <Shadow y={150} rx={44} />
    <path className="cn-dash" d="M58 66 Q60 10 100 12 Q140 10 142 66" fill="none" />
    <g className="cn-jg">
      <g className="cn-ball b1"><circle className="cn-a cn-o" cx="62" cy="40" r="11" /><path className="cn-dks" d="M62 35v5l3 2" /></g>
      <g className="cn-ball b2"><circle className="cn-g cn-o" cx="100" cy="14" r="11" /><path className="cn-lt-s" d="M95 14l3 3 6-6" /></g>
      <g className="cn-ball b3"><circle className="cn-i cn-o" cx="138" cy="40" r="11" /><path className="cn-lt-s" d="M133 37h10M133 42h6" /></g>
    </g>
    <path className="cn-o" fill="none" d="M66 96 Q52 84 58 66" />
    <path className="cn-o" fill="none" d="M134 96 Q148 84 142 66" />
    <g className="cn-bob">
      <rect className="cn-lt cn-o" x="62" y="66" width="76" height="72" rx="12" />
      <path className="cn-p cn-o" d="M62 80 V78 a12 12 0 0 1 12-12 h52 a12 12 0 0 1 12 12 v6 H62Z" />
      <rect className="cn-dk cn-o" x="78" y="58" width="6" height="16" rx="3" /><rect className="cn-dk cn-o" x="116" y="58" width="6" height="16" rx="3" />
      <g className="cn-md-f">{[0, 1, 2, 3].map(i => <rect key={i} x={71 + i * 15} y={122} width={9} height={7} rx={2} />)}</g>
      <rect className="cn-a" x="116" y="122" width="9" height="7" rx="2" />
      <circle className="cn-dkf" cx="88" cy="100" r="3" /><circle className="cn-dkf" cx="112" cy="100" r="3" />
      <path className="cn-dks" d="M90 112 q10 -6 20 0" />
      <path className="cn-sw" d="M128 92 q4 6 0 9 q-4 -3 0 -9Z" />
    </g>
    <path className="cn-o" fill="none" d="M88 138 v10 h-8 M112 138 v10 h8" />
  </Svg>
}

/** A magnifying-glass detective (empty search). */
export function Detective(p: P) {
  return <Svg {...p}>
    <Shadow y={150} rx={50} />
    <g className="cn-ft"><ellipse className="cn-md-f" cx="30" cy="140" rx="6" ry="3.5" /><ellipse className="cn-md-f" cx="48" cy="132" rx="6" ry="3.5" /><ellipse className="cn-md-f" cx="64" cy="142" rx="6" ry="3.5" /></g>
    <g className="cn-bob">
      <path className="cn-dk cn-o" d="M128 104 L162 140" strokeWidth="13" strokeLinecap="round" />
      <path className="cn-pd" d="M128 104 L162 140" style={{ stroke: 'var(--cn-pd)', strokeWidth: 8, strokeLinecap: 'round' }} />
      <circle className="cn-md cn-o" cx="100" cy="76" r="44" />
      <circle className="cn-gl cn-o" cx="100" cy="76" r="35" />
      <path className="cn-hl" d="M76 62 Q82 48 96 46" />
      {/* magnified eye */}
      <ellipse className="cn-lt cn-o" cx="100" cy="80" rx="17" ry="14" />
      <circle className="cn-dkf cn-look" cx="104" cy="82" r="7" />
      <circle className="cn-lt" cx="106.5" cy="79.5" r="2.2" />
      <path className="cn-dks" d="M83 62 q17 -8 34 2" />
      {/* hat */}
      <path className="cn-pd cn-o" d="M62 40 Q70 14 100 14 Q130 14 138 40 Q100 30 62 40Z" />
      <path className="cn-a cn-o" d="M66 37 Q100 26 134 37 L136 42 Q100 32 64 42Z" />
      <path className="cn-pd cn-o" d="M58 42 Q100 30 142 42 Q100 50 58 42Z" />
      {/* moustache */}
      <path className="cn-hr cn-o" d="M100 108 q-10 -8 -22 2 q8 4 22 -2 q14 6 22 2 q-12 -10 -22 -2Z" />
    </g>
    <text className="cn-q" x="160" y="44">?</text>
    <text className="cn-q d2" x="28" y="60">?</text>
  </Svg>
}

/** A sleepy cat on a cushion (no activity). */
export function SleepyCat(p: P) {
  return <Svg {...p}>
    <ellipse className="cn-p cn-o" cx="100" cy="128" rx="74" ry="18" />
    <g className="cn-breathe">
      <path className="cn-a cn-o" d="M150 116 Q178 112 170 90 Q166 80 158 86 Q166 98 146 106Z" />
      <ellipse className="cn-a cn-o" cx="104" cy="106" rx="50" ry="26" />
      <path className="cn-wn" d="M86 86 q4 10 0 20 M102 82 q4 10 0 22 M118 84 q4 10 0 20" style={{ fill: 'none', stroke: 'var(--cn-tab)', strokeWidth: 4, strokeLinecap: 'round' }} />
      <path className="cn-a cn-o" d="M52 76 L58 56 L70 70Z" />
      <path className="cn-a cn-o" d="M80 70 L90 52 L94 72Z" />
      <circle className="cn-a cn-o" cx="72" cy="94" r="24" />
      <path className="cn-pk" d="M58 70 L61 61 L66 68Z" /><path className="cn-pk" d="M84 69 L89 59 L91 70Z" />
      <path className="cn-dks" d="M60 92 q5 4 10 0 M76 92 q5 4 10 0" />
      <path className="cn-pk cn-o" d="M71 100 l3 0 -1.5 2.5Z" strokeWidth="1.5" />
      <path className="cn-dks" d="M72.5 103 q-3 4 -7 2 M72.5 103 q3 4 7 2" />
      <path className="cn-ln" d="M48 98 h-12 M50 104 l-12 4 M96 98 h12 M94 104 l12 4" />
      <ellipse className="cn-a cn-o" cx="62" cy="118" rx="10" ry="6" />
    </g>
    <g className="cn-zz"><text x="112" y="58" className="z1">z</text><text x="128" y="42" className="z2">z</text><text x="146" y="26" className="z3">Z</text></g>
  </Svg>
}

/** A celebrating trophy with confetti (wins). */
export function Trophy(p: P) {
  return <Svg {...p}>
    <Shadow y={150} rx={44} />
    <g className="cn-conf">
      <rect className="cn-p" x="30" y="30" width="8" height="5" rx="1" transform="rotate(25 34 32)" />
      <rect className="cn-i" x="160" y="40" width="8" height="5" rx="1" transform="rotate(-30 164 42)" />
      <rect className="cn-g" x="150" y="96" width="8" height="5" rx="1" transform="rotate(40 154 98)" />
      <circle className="cn-a" cx="44" cy="88" r="4" /><circle className="cn-p" cx="172" cy="70" r="3.5" />
      <rect className="cn-a" x="56" y="16" width="7" height="4" rx="1" transform="rotate(-20 60 18)" />
      <rect className="cn-g" x="140" y="14" width="7" height="4" rx="1" transform="rotate(20 144 16)" />
      <circle className="cn-i" cx="24" cy="120" r="3" />
    </g>
    <g className="cn-bob">
      <path className="cn-o" fill="none" d="M66 56 Q40 40 40 18" />
      <path className="cn-o" fill="none" d="M134 56 Q160 40 160 18" />
      <circle className="cn-k cn-o" cx="40" cy="16" r="6" /><circle className="cn-k cn-o" cx="160" cy="16" r="6" />
      <path className="cn-a cn-o" d="M70 50 Q50 50 52 68 Q54 84 76 86" fill="none" strokeWidth="6" style={{ stroke: 'var(--acc)' }} />
      <path className="cn-a cn-o" d="M130 50 Q150 50 148 68 Q146 84 124 86" fill="none" strokeWidth="6" style={{ stroke: 'var(--acc)' }} />
      <path className="cn-a cn-o" d="M66 38 H134 V64 Q134 104 100 108 Q66 104 66 64Z" />
      <path className="cn-hl" d="M76 46 V70 Q76 86 86 94" />
      <path className="cn-dks" d="M84 66 q5 -6 10 0 M106 66 q5 -6 10 0" />
      <path className="cn-dkf" d="M88 78 Q100 94 112 78Z" />
      <path className="cn-p" d="M95 86 q5 4 10 0 q-5 6 -10 0Z" />
      <rect className="cn-a cn-o" x="92" y="108" width="16" height="14" />
      <rect className="cn-dk cn-o" x="72" y="122" width="56" height="20" rx="5" />
      <rect className="cn-a" x="88" y="128" width="24" height="8" rx="2" />
    </g>
  </Svg>
}

/** A flying piggy bank catching a coin (billing). */
export function PiggyBank(p: P) {
  return <Svg {...p}>
    <Shadow y={150} rx={50} />
    <g className="cn-flt"><circle className="cn-a cn-o" cx="104" cy="22" r="11" /><path className="cn-dks" d="M104 16v12" /></g>
    <g className="cn-bob">
      <path className="cn-lt cn-o cn-wing" d="M92 62 Q78 30 52 34 Q66 46 62 52 Q48 50 44 60 Q62 60 70 70Z" />
      <path className="cn-tail" d="M48 98 q-12 -2 -10 -10 q2 -6 8 -2" fill="none" />
      <ellipse className="cn-pg cn-o" cx="100" cy="96" rx="52" ry="38" />
      <path className="cn-pg cn-o" d="M70 64 L66 46 L84 58Z" />
      <rect className="cn-pd" x="94" y="60" width="22" height="5" rx="2.5" />
      <ellipse className="cn-pgd cn-o" cx="150" cy="98" rx="11" ry="13" />
      <circle className="cn-pd" cx="147" cy="94" r="2" /><circle className="cn-pd" cx="152" cy="102" r="2" />
      <circle className="cn-dkf" cx="128" cy="84" r="3.2" />
      <path className="cn-dks" d="M122 108 q8 6 16 0" />
      <ellipse className="cn-pk" cx="130" cy="98" rx="6" ry="3.5" />
      <rect className="cn-pgd cn-o" x="72" y="124" width="12" height="16" rx="4" />
      <rect className="cn-pgd cn-o" x="114" y="124" width="12" height="16" rx="4" />
    </g>
    <g className="cn-flt d2"><rect className="cn-lt cn-o" x="150" y="30" width="34" height="22" rx="4" /><rect className="cn-i" x="150" y="36" width="34" height="5" /><rect className="cn-a" x="155" y="44" width="8" height="5" rx="1" /></g>
  </Svg>
}
