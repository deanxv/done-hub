import PropTypes from 'prop-types';
import { motion, useReducedMotion } from 'framer-motion';

// Scroll-triggered enter animation wrapper.
// Mirrors the framer-motion usage in src/layout/NavMotion.js, but fires on
// viewport entry and respects the user's reduced-motion preference.
const AnimateInView = ({ children, delay = 0, y = 24, once = true, ...other }) => {
  const shouldReduceMotion = useReducedMotion();

  if (shouldReduceMotion) {
    return <div {...other}>{children}</div>;
  }

  return (
    <motion.div
      initial={{ opacity: 0, y }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once, amount: 0.2 }}
      transition={{ duration: 0.7, delay, ease: [0.16, 1, 0.3, 1] }}
      {...other}
    >
      {children}
    </motion.div>
  );
};

AnimateInView.propTypes = {
  children: PropTypes.node,
  delay: PropTypes.number,
  y: PropTypes.number,
  once: PropTypes.bool
};

export default AnimateInView;
