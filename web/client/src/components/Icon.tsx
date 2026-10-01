// The line icons of the mockups (docs/mockups/generate.py icon()), drawn on a
// 16-unit grid with the current text colour, so a control's colour is set in
// CSS once for its label and its icon.
const paths = {
  search: <><circle cx="7" cy="7" r="4.5" /><path d="M10.5 10.5 14 14" /></>,
  inbox: <><path d="M2 9h3.5l1 2h3l1-2H14" /><path d="M3.5 3h9l1.5 6v4H2V9z" /></>,
  home: <><path d="M2.5 7.5 8 3l5.5 4.5V13a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5z" /><path d="M6.5 13.5v-4h3v4" /></>,
  list: (
    <>
      <path d="M5 4h9M5 8h9M5 12h9" />
      <circle cx="2.5" cy="4" r=".75" />
      <circle cx="2.5" cy="8" r=".75" />
      <circle cx="2.5" cy="12" r=".75" />
    </>
  ),
  clock: <><circle cx="8" cy="8" r="5.5" /><path d="M8 5v3.5l2.5 1.5" /></>,
  bell: <><path d="M4 11V7.5a4 4 0 0 1 8 0V11l1 1.5H3z" /><path d="M6.5 13.5a1.5 1.5 0 0 0 3 0" /></>,
  user: <><circle cx="8" cy="5.5" r="2.75" /><path d="M2.75 14a5.25 5.25 0 0 1 10.5 0" /></>,
  more: <><circle cx="3.5" cy="8" r="1" /><circle cx="8" cy="8" r="1" /><circle cx="12.5" cy="8" r="1" /></>,
  back: <path d="m10 3-5 5 5 5" />,
  reply: <><path d="M6 4 2 8l4 4" /><path d="M2 8h7a5 5 0 0 1 5 5v1" /></>,
  bold: <path d="M5 3h4a2.5 2.5 0 0 1 0 5H5zM5 8h4.5a2.5 2.5 0 0 1 0 5H5z" />,
  italic: <path d="M7 3h6M3 13h6M10 3 6 13" />,
  quote: <path d="M4 4h3v4H5v3H3V6zM10 4h3v4h-2v3H9V6z" />,
  code: <path d="m5 5-3 3 3 3M11 5l3 3-3 3M9.5 3l-3 10" />,
  at: <><circle cx="8" cy="8" r="2.5" /><path d="M10.5 8v1a1.5 1.5 0 0 0 3 0V8a5.5 5.5 0 1 0-2.2 4.4" /></>,
  link: (
    <path d="M6.5 9.5 9.5 6.5M7 4.5l1-1a2.5 2.5 0 0 1 3.5 3.5l-1 1M9 11.5l-1 1a2.5 2.5 0 0 1-3.5-3.5l1-1" />
  ),
  lock: <><rect x="3.5" y="7" width="9" height="7" rx="1" /><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" /></>,
  pen: <><path d="m3 13 .8-3.2L10.6 3l2.4 2.4-6.8 6.8z" /><path d="m9.5 4.2 2.3 2.3" /></>,
  pin: <path d="M6 2h4l-.5 4 2.5 2.5V10H4V8.5L6.5 6zM8 10v4" />,
  clip: <path d="m13 7.5-5 5a3 3 0 0 1-4.5-4.5l5.5-5.5a2 2 0 0 1 3 3L6.5 11a1 1 0 0 1-1.5-1.5L10 4.5" />,
  file: <><path d="M4 2h5l3 3v9H4z" /><path d="M9 2v3h3" /></>,
};

export type IconName = keyof typeof paths;

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  return (
    <svg
      class="icon"
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}
