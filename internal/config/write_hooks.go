// Thin wrappers around internal/asteriskconf's own write primitives,
// each notifying Store.OnChange on success -- see that field's own doc
// comment for why every write in this package goes through one of these
// instead of calling asteriskconf directly.
package config

import "hamvoipconfiggui-asl3/internal/asteriskconf"

func (s *Store) setValues(path, section string, updates map[string]string) error {
	if err := asteriskconf.SetValues(path, section, updates); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

func (s *Store) createSection(path, name string, inherits []string, pairs []asteriskconf.Pair) error {
	if err := asteriskconf.CreateSection(path, name, inherits, pairs); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

func (s *Store) removeSection(path, name string) error {
	if err := asteriskconf.RemoveSection(path, name); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

func (s *Store) removeValue(path, section, key string) error {
	if err := asteriskconf.RemoveValue(path, section, key); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

func (s *Store) setRepeatingValue(path, section, key, valuePrefix, newValue string) error {
	if err := asteriskconf.SetRepeatingValue(path, section, key, valuePrefix, newValue); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

func (s *Store) removeRepeatingValue(path, section, key, valuePrefix string) error {
	if err := asteriskconf.RemoveRepeatingValue(path, section, key, valuePrefix); err != nil {
		return err
	}
	s.notifyChanged()
	return nil
}

// setNthValueInSection only notifies when ok is true -- a false result
// (no key at that position) changed nothing on disk.
func (s *Store) setNthValueInSection(path, section string, n int, newValue string) (bool, error) {
	ok, err := asteriskconf.SetNthValueInSection(path, section, n, newValue)
	if err != nil {
		return ok, err
	}
	if ok {
		s.notifyChanged()
	}
	return ok, nil
}
