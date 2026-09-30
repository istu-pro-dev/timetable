// Russian UI strings (the default and, for now, only locale). Keys are typed: a missing key
// is a compile error. `{name}` placeholders are filled by t(key, { name }).
export const ru = {
  app: {
    title: 'Расписание',
    subtitle: 'Система составления расписания',
    loading: 'Загрузка…',
    skipToContent: 'Перейти к содержимому',
  },
  nav: {
    main: 'Навигация',
    home: 'Главная',
    management: 'Справочники',
    buildings: 'Корпуса',
    roomTypes: 'Типы аудиторий',
    rooms: 'Аудитории',
    groups: 'Группы',
    teachers: 'Преподаватели',
    disciplines: 'Дисциплины',
    timeGrid: 'Сетка занятий',
    curriculum: 'Учебный план',
    dev: 'Разработка',
    gridDemo: 'Сетка (демо)',
    openMenu: 'Открыть меню',
    closeMenu: 'Закрыть меню',
  },
  roles: {
    student: 'Студент',
    teacher: 'Преподаватель',
    admin: 'Администратор',
    ai_agent: 'ИИ-агент',
  },
  auth: {
    loginTitle: 'Вход в систему',
    login: 'Логин',
    password: 'Пароль',
    submit: 'Войти',
    submitting: 'Вход…',
    logout: 'Выйти',
    invalidCredentials: 'Неверный логин или пароль',
    accountDisabled: 'Учётная запись отключена',
    tooManyAttempts: 'Слишком много попыток. Повторите через {seconds} с.',
    tooManyAttemptsNoDelay: 'Слишком много попыток. Повторите позже.',
    required: 'Введите логин и пароль',
    sessionExpired: 'Сессия истекла, войдите снова',
  },
  theme: {
    label: 'Тема оформления',
    light: 'Светлая',
    dark: 'Тёмная',
    system: 'Системная',
  },
  home: {
    welcome: 'Здравствуйте, {name}!',
    apiStatus: 'API',
    apiChecking: 'проверка…',
    apiOk: 'работает',
    apiDown: 'недоступен',
  },
  common: {
    comingSoon: 'Раздел в разработке',
    notFound: 'Страница не найдена',
    forbidden: 'Недостаточно прав для этого раздела',
    toHome: 'На главную',
    close: 'Закрыть',
    cancel: 'Отмена',
    retry: 'Повторить',
    errorTitle: 'Ошибка',
    genericError: 'Что-то пошло не так',
    networkError: 'Сервер недоступен',
  },
  // Keyed by the API error code ({"error":{"code"}}), see backend/api/openapi.yaml.
  errors: {
    invalid_request: 'Некорректный запрос',
    unauthorized: 'Требуется вход в систему',
    forbidden: 'Недостаточно прав',
    not_found: 'Запись не найдена',
    already_exists: 'Запись с такими данными уже существует',
    in_use: 'Запись используется в других данных и не может быть удалена',
    schedule_conflict: 'Изменение приводит к конфликтам в расписании',
    validation_failed: 'Некорректные значения полей',
    invalid_reference: 'Связанная запись не найдена',
    constraint_violation: 'Нарушено ограничение данных',
    no_database: 'Сервер работает без базы данных',
  },
} as const

type Widen<T> = { [K in keyof T]: T[K] extends string ? string : Widen<T[K]> }

/** The shape every locale dictionary must have. */
export type Messages = Widen<typeof ru>
